import type { Page } from "@playwright/test";
import { test, expect } from "./accounts-fixture";
import http from "node:http";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const screenshotsDir = path.join(__dirname, "__screenshots__", "cf-06");

const courseId = "6f1d2b9e-0c3a-4b7e-9d1a-2f4c8e5a7b01";
const edgePort = Number(process.env.E2E_EDGE_CHAT_PORT ?? 4198);
const edgeChatOrigin = `http://127.0.0.1:${edgePort}`;

const sse = (...events: [string, unknown][]) =>
  events
    .map(([name, data]) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`)
    .join("");

function makeBrief(topic: string) {
  return {
    topic,
    goal: "",
    include_topics: "",
    skip_topics: "",
    level: "",
    maths: "",
    viewing_conditions: "",
    lesson_length: "",
    language: "",
  };
}

interface FakeServerState {
  mode: "ws_ok" | "ws_blocked" | "edge_unavailable";
  wsConnections: number;
  wsMessages: unknown[];
  sseMessagesCalls: number;
  fallbackCalls: unknown[];
  guidedTurns: unknown[];
  lastTurnKey?: string;
}

function encodeWsTextFrame(payload: string): Buffer {
  const data = Buffer.from(payload, "utf8");
  const len = data.length;
  if (len < 126) {
    return Buffer.concat([Buffer.from([0x81, len]), data]);
  } else if (len <= 65535) {
    const header = Buffer.alloc(4);
    header[0] = 0x81;
    header[1] = 126;
    header.writeUInt16BE(len, 2);
    return Buffer.concat([header, data]);
  } else {
    const header = Buffer.alloc(10);
    header[0] = 0x81;
    header[1] = 127;
    header.writeBigUInt64BE(BigInt(len), 2);
    return Buffer.concat([header, data]);
  }
}

function parseWsFrames(
  buffer: Buffer,
  onMessage: (msg: string) => void,
): Buffer {
  let buf = buffer;
  while (buf.length >= 2) {
    const firstByte = buf[0];
    const secondByte = buf[1];
    const opcode = firstByte & 0x0f;
    const isMasked = (secondByte & 0x80) !== 0;
    let payloadLen = secondByte & 0x7f;
    let offset = 2;

    if (payloadLen === 126) {
      if (buf.length < 4) break;
      payloadLen = buf.readUInt16BE(2);
      offset = 4;
    } else if (payloadLen === 127) {
      if (buf.length < 10) break;
      payloadLen = Number(buf.readBigUInt64BE(2));
      offset = 10;
    }

    let maskKey: Buffer | null = null;
    if (isMasked) {
      if (buf.length < offset + 4) break;
      maskKey = buf.subarray(offset, offset + 4);
      offset += 4;
    }

    if (buf.length < offset + payloadLen) break;

    const payload = Buffer.from(buf.subarray(offset, offset + payloadLen));
    if (maskKey) {
      for (let i = 0; i < payload.length; i++) {
        payload[i] ^= maskKey[i % 4];
      }
    }

    buf = buf.subarray(offset + payloadLen);

    if (opcode === 0x01) {
      // text
      onMessage(payload.toString("utf8"));
    } else if (opcode === 0x08) {
      // close
      break;
    }
  }
  return buf;
}

let fakeServer: http.Server | undefined;
const activeSockets = new Set<import("net").Socket>();
const serverState: FakeServerState = {
  mode: "ws_ok",
  wsConnections: 0,
  wsMessages: [],
  sseMessagesCalls: 0,
  fallbackCalls: [],
  guidedTurns: [],
};

test.beforeAll(async () => {
  fs.mkdirSync(screenshotsDir, { recursive: true });

  fakeServer = http.createServer((req, res) => {
    // CORS headers
    res.setHeader("Access-Control-Allow-Origin", "*");
    res.setHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
    res.setHeader(
      "Access-Control-Allow-Headers",
      "Authorization, Content-Type, Last-Event-ID",
    );

    if (req.method === "OPTIONS") {
      res.writeHead(204);
      res.end();
      return;
    }

    if (serverState.mode === "edge_unavailable") {
      res.writeHead(503, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "edge_unavailable" }));
      return;
    }

    if (req.method === "POST" && req.url === "/v1/edge/chat/fallback") {
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        try {
          serverState.fallbackCalls.push(JSON.parse(body));
        } catch {
          // ignore
        }
        res.writeHead(204);
        res.end();
      });
      return;
    }

    if (req.method === "POST" && req.url === "/v1/edge/chat/messages") {
      serverState.sseMessagesCalls++;
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        try {
          const parsed = JSON.parse(body);
          serverState.lastTurnKey = parsed.idempotency_key;
        } catch {
          // ignore
        }
        res.writeHead(200, {
          "Content-Type": "text/event-stream",
          "Cache-Control": "no-store",
        });
        res.write(
          sse(
            ["thread", { thread_id: "thread-sse-1" }],
            ["delta", { text: "Hello from edge SSE fallback!" }],
            [
              "done",
              {
                message_id: "msg-sse-1",
                thread_id: "thread-sse-1",
                replayed: false,
              },
            ],
          ),
        );
        res.end();
      });
      return;
    }

    if (req.method === "POST" && req.url === "/v1/edge/chat/guided-turn") {
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        try {
          const parsed = JSON.parse(body);
          serverState.guidedTurns.push(parsed);
          res.writeHead(200, { "Content-Type": "application/json" });
          res.end(
            JSON.stringify({
              message: "Goal via Edge HTTP fallback",
              quick_replies: [{ id: "q1", label: "Research" }],
              next_step: "goal",
              is_follow_up: false,
              is_complete: false,
              brief: makeBrief(parsed?.answer || "Quantum Physics"),
            }),
          );
        } catch {
          res.writeHead(400);
          res.end();
        }
      });
      return;
    }

    if (req.method === "GET" && req.url?.startsWith("/v1/edge/chat/turns/")) {
      const key = req.url.slice("/v1/edge/chat/turns/".length);
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(
        JSON.stringify({
          state: "completed",
          message: {
            id: "turn-" + key,
            content: "Recovered partial",
            status: "interrupted",
          },
        }),
      );
      return;
    }

    if (req.method === "POST" && req.url?.includes("/stop")) {
      res.writeHead(202, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ status: "stopped" }));
      return;
    }

    res.writeHead(404);
    res.end();
  });

  fakeServer.on("connection", (socket) => {
    activeSockets.add(socket);
    socket.on("close", () => activeSockets.delete(socket));
  });

  fakeServer.on("upgrade", (req, socket, head) => {
    activeSockets.add(socket);
    socket.on("close", () => activeSockets.delete(socket));
    if (
      serverState.mode === "ws_blocked" ||
      serverState.mode === "edge_unavailable"
    ) {
      socket.write("HTTP/1.1 503 Service Unavailable\r\n\r\n");
      socket.destroy();
      return;
    }

    const key = req.headers["sec-websocket-key"];
    if (!key) {
      socket.destroy();
      return;
    }
    const accept = crypto
      .createHash("sha1")
      .update(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
      .digest("base64");

    socket.write(
      "HTTP/1.1 101 Switching Protocols\r\n" +
        "Upgrade: websocket\r\n" +
        "Connection: Upgrade\r\n" +
        `Sec-WebSocket-Accept: ${accept}\r\n` +
        "Sec-WebSocket-Protocol: duolify.chat.v1\r\n\r\n",
    );

    serverState.wsConnections++;
    let rxBuffer = head ? Buffer.from(head) : Buffer.alloc(0);

    socket.on("data", (chunk) => {
      rxBuffer = Buffer.concat([rxBuffer, chunk]);
      rxBuffer = parseWsFrames(rxBuffer, (text) => {
        try {
          const msg = JSON.parse(text);
          serverState.wsMessages.push(msg);
          if (msg.op === "send") {
            serverState.lastTurnKey = msg.body?.idempotency_key;
            // Send stream frames
            socket.write(
              encodeWsTextFrame(
                JSON.stringify({
                  id: msg.id,
                  event: "thread",
                  data: { thread_id: "thread-ws-1" },
                  seq: 1,
                }),
              ),
            );
            socket.write(
              encodeWsTextFrame(
                JSON.stringify({
                  id: msg.id,
                  event: "delta",
                  data: { text: "Hello from edge WebSocket!" },
                  seq: 2,
                }),
              ),
            );
            socket.write(
              encodeWsTextFrame(
                JSON.stringify({
                  id: msg.id,
                  event: "done",
                  data: {
                    message_id: "msg-ws-1",
                    thread_id: "thread-ws-1",
                    replayed: false,
                  },
                  seq: 3,
                }),
              ),
            );
          } else if (msg.op === "guided") {
            serverState.guidedTurns.push(msg.body);
            socket.write(
              encodeWsTextFrame(
                JSON.stringify({
                  id: msg.id,
                  event: "result",
                  status: 200,
                  data: {
                    message: "Goal via Edge WebSocket",
                    quick_replies: [{ id: "q1", label: "Research" }],
                    next_step: "goal",
                    is_follow_up: false,
                    is_complete: false,
                    brief: makeBrief(msg.body?.answer || "Quantum Physics"),
                  },
                }),
              ),
            );
          } else if (msg.op === "stop") {
            socket.write(
              encodeWsTextFrame(
                JSON.stringify({
                  id: msg.id,
                  event: "result",
                  status: 202,
                  data: { status: "stopped" },
                }),
              ),
            );
          } else if (msg.op === "recover") {
            socket.write(
              encodeWsTextFrame(
                JSON.stringify({
                  id: msg.id,
                  event: "result",
                  status: 200,
                  data: {
                    state: "completed",
                    message: {
                      id: "recovered-1",
                      content: "Recovered partial",
                      status: "interrupted",
                    },
                  },
                }),
              ),
            );
          }
        } catch {
          // ignore
        }
      });
    });
  });

  await new Promise<void>((resolve) =>
    fakeServer!.listen(edgePort, "127.0.0.1", () => resolve()),
  );
});

test.afterAll(async () => {
  if (fakeServer) {
    for (const socket of activeSockets) {
      try {
        socket.destroy();
      } catch {
        // ignore
      }
    }
    activeSockets.clear();
    await new Promise<void>((resolve) => fakeServer!.close(() => resolve()));
  }
});

test.beforeEach(() => {
  serverState.mode = "ws_ok";
  serverState.wsConnections = 0;
  serverState.wsMessages = [];
  serverState.sseMessagesCalls = 0;
  serverState.fallbackCalls = [];
  serverState.guidedTurns = [];
  delete serverState.lastTurnKey;
});

async function mockCommonRoutes(
  page: Page,
  options: { chatEdgeEnabled: boolean },
) {
  const legacySent: Record<string, unknown>[] = [];
  const edgeChannelCalls: Record<string, unknown>[] = [];
  const legacyGuidedCalls: Record<string, unknown>[] = [];
  const guidedEdgeChannelCalls: Record<string, unknown>[] = [];

  await page.addInitScript(
    ({ edgeUrl, edgeEnabled }) => {
      (window as unknown as { __EDGE_CHAT_URL__?: string }).__EDGE_CHAT_URL__ =
        edgeUrl;
      (
        window as unknown as { __CHAT_EDGE_ENABLED__?: boolean }
      ).__CHAT_EDGE_ENABLED__ = edgeEnabled;
    },
    { edgeUrl: edgeChatOrigin, edgeEnabled: options.chatEdgeEnabled },
  );

  await page.route("**/v1/features", (route) =>
    route.fulfill({
      json: {
        lesson_test_tracer_enabled: true,
        marketplace_enabled: false,
        chat_edge_enabled: options.chatEdgeEnabled,
      },
    }),
  );

  await page.route(`**/v1/courses/${courseId}`, (route) =>
    route.fulfill({
      json: {
        id: courseId,
        title: "Astronomy",
        description: "Look up",
        units: [
          {
            title: "Sky",
            lessons: [{ id: "l1", title: "Moon", state: "current" }],
          },
        ],
      },
    }),
  );

  await page.route("**/v1/me/credits", (route) =>
    route.fulfill({
      json: {
        available: 150,
        daily_free: {
          available: 20,
          granted: 30,
          resets_at: "2030-01-01T00:00:00Z",
          timezone: "UTC",
        },
        purchased: 130,
        subscription: 0,
        reserved: 0,
        recent_activity: [],
      },
    }),
  );

  await page.route(`**/v1/courses/${courseId}/tutor/thread`, (route) =>
    route.fulfill({ json: { thread: null, messages: [] } }),
  );

  await page.route(`**/v1/courses/${courseId}/tutor/quote`, (route) =>
    route.fulfill({
      json: {
        id: "quote-1",
        content_ceiling: 5,
        pricing_version: "unapproved-test",
        daily_free_eligible: true,
        expires_at: new Date(Date.now() + 600000).toISOString(),
      },
    }),
  );

  await page.route("**/v1/quotes/*/accept", (route) =>
    route.fulfill({ json: { accepted: true } }),
  );

  await page.route(`**/v1/courses/${courseId}/tutor/edge-channel`, (route) => {
    edgeChannelCalls.push(
      route.request().postDataJSON() as Record<string, unknown>,
    );
    return route.fulfill({
      json: {
        token: "fake.edge.jwt",
        expires_at: Math.floor(Date.now() / 1000) + 300,
      },
    });
  });

  await page.route(`**/v1/courses/${courseId}/tutor/messages`, (route) => {
    legacySent.push(route.request().postDataJSON() as Record<string, unknown>);
    return route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: sse(
        ["thread", { thread_id: "legacy-t1" }],
        ["delta", { text: "Hello from legacy Go/engine SSE!" }],
        [
          "done",
          { message_id: "legacy-m1", thread_id: "legacy-t1", replayed: false },
        ],
      ),
    });
  });

  await page.route("**/v1/me/guided-chat/edge-channel", (route) => {
    guidedEdgeChannelCalls.push(
      route.request().postDataJSON() as Record<string, unknown>,
    );
    return route.fulfill({
      json: {
        token: "fake.guided.edge.jwt",
        expires_at: Math.floor(Date.now() / 1000) + 300,
      },
    });
  });

  await page.route("**/v1/course-brief/config", (route) =>
    route.fulfill({
      json: {
        enabled: true,
        credits: 0,
        pricing_version: "v1",
      },
    }),
  );

  await page.route("**/v1/course-brief/chat", (route) => {
    legacyGuidedCalls.push(
      route.request().postDataJSON() as Record<string, unknown>,
    );
    return route.fulfill({
      json: {
        message: "Goal via Legacy Go",
        quick_replies: [{ id: "q1", label: "Research" }],
        next_step: "goal",
        is_follow_up: false,
        is_complete: false,
        brief: makeBrief("Quantum Physics"),
      },
    });
  });

  return {
    legacySent,
    edgeChannelCalls,
    legacyGuidedCalls,
    guidedEdgeChannelCalls,
  };
}

test.describe("CF-06 client tutor chat transport & fallbacks", () => {
  test("chat over edge WebSocket when chat_edge_enabled is true", async ({
    page,
  }) => {
    serverState.mode = "ws_ok";
    const mocks = await mockCommonRoutes(page, { chatEdgeEnabled: true });

    // Desktop viewport: 1280x800
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`/course/${courseId}`);
    await page.getByRole("button", { name: "Ask the tutor" }).click();
    const panel = page.getByRole("complementary", { name: "Tutor chat" });
    await expect(panel).toBeVisible();

    const input = panel.getByRole("textbox", { name: "Message" });
    await input.fill("What is the Moon?");
    await expect(panel.getByText(/up to 5 credits/i)).toBeVisible();
    await input.press("Enter");

    // Verify reply from edge WebSocket
    await expect(panel.getByText("Hello from edge WebSocket!")).toBeVisible();
    expect(serverState.wsConnections).toBeGreaterThan(0);
    expect(mocks.edgeChannelCalls.length).toBeGreaterThan(0);
    expect(mocks.legacySent.length).toBe(0);

    // Save desktop screenshot
    await page.screenshot({
      path: path.join(screenshotsDir, "desktop-websocket.png"),
    });

    // Mobile viewport: 390x844
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(panel.getByText("Hello from edge WebSocket!")).toBeVisible();
    await page.screenshot({
      path: path.join(screenshotsDir, "mobile-websocket.png"),
    });
  });

  test("WebSocket upgrade blocked -> falls back to Edge SSE", async ({
    page,
  }) => {
    serverState.mode = "ws_blocked";
    const mocks = await mockCommonRoutes(page, { chatEdgeEnabled: true });

    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`/course/${courseId}`);
    await page.getByRole("button", { name: "Ask the tutor" }).click();
    const panel = page.getByRole("complementary", { name: "Tutor chat" });
    await expect(panel).toBeVisible();

    const input = panel.getByRole("textbox", { name: "Message" });
    await input.fill("Why is the sky blue?");
    await expect(panel.getByText(/up to 5 credits/i)).toBeVisible();
    await input.press("Enter");

    // Expect response over Edge SSE fallback
    await expect(
      panel.getByText("Hello from edge SSE fallback!"),
    ).toBeVisible();
    expect(serverState.sseMessagesCalls).toBe(1);
    expect(mocks.legacySent.length).toBe(0);

    // Save desktop fallback screenshot
    await page.screenshot({
      path: path.join(screenshotsDir, "desktop-fallback.png"),
    });

    // Mobile viewport
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(
      panel.getByText("Hello from edge SSE fallback!"),
    ).toBeVisible();
    await page.screenshot({
      path: path.join(screenshotsDir, "mobile-fallback.png"),
    });
  });

  test("Edge unavailable -> falls back to legacy Go/engine path", async ({
    page,
  }) => {
    serverState.mode = "edge_unavailable";
    const mocks = await mockCommonRoutes(page, { chatEdgeEnabled: true });

    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`/course/${courseId}`);
    await page.getByRole("button", { name: "Ask the tutor" }).click();
    const panel = page.getByRole("complementary", { name: "Tutor chat" });
    await expect(panel).toBeVisible();

    const input = panel.getByRole("textbox", { name: "Message" });
    await input.fill("How far is Mars?");
    await expect(panel.getByText(/up to 5 credits/i)).toBeVisible();
    await input.press("Enter");

    // Expect response over legacy Go SSE path
    await expect(
      panel.getByText("Hello from legacy Go/engine SSE!"),
    ).toBeVisible();
    expect(mocks.legacySent.length).toBe(1);
  });

  test("Flag off (chat_edge_enabled: false) -> uses legacy path directly without calling edge", async ({
    page,
  }) => {
    const mocks = await mockCommonRoutes(page, { chatEdgeEnabled: false });

    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`/course/${courseId}`);
    await page.getByRole("button", { name: "Ask the tutor" }).click();
    const panel = page.getByRole("complementary", { name: "Tutor chat" });
    await expect(panel).toBeVisible();

    const input = panel.getByRole("textbox", { name: "Message" });
    await input.fill("Tell me about Venus");
    await expect(panel.getByText(/up to 5 credits/i)).toBeVisible();
    await input.press("Enter");

    await expect(
      panel.getByText("Hello from legacy Go/engine SSE!"),
    ).toBeVisible();
    expect(mocks.legacySent.length).toBe(1);
    expect(mocks.edgeChannelCalls.length).toBe(0);
    expect(serverState.wsConnections).toBe(0);
    expect(serverState.sseMessagesCalls).toBe(0);
  });

  test("guided course brief routes over edge WebSocket when chat_edge_enabled is true", async ({
    page,
  }) => {
    serverState.mode = "ws_ok";
    const mocks = await mockCommonRoutes(page, { chatEdgeEnabled: true });

    await page.addInitScript(() => {
      try {
        localStorage.setItem("duolify.guided.course_brief", "off");
      } catch {
        // ignore
      }
    });

    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto("/courses/create");

    const input = page.getByRole("textbox", {
      name: "Your answer",
    });
    await expect(input).toBeVisible();
    await input.fill("Quantum Physics");
    await page.getByRole("button", { name: "Send answer" }).click();

    await expect(
      page.getByText("Goal via Edge WebSocket", { exact: true }),
    ).toBeVisible();
    expect(mocks.guidedEdgeChannelCalls.length).toBeGreaterThan(0);
    expect(mocks.legacyGuidedCalls.length).toBe(0);
    expect(serverState.guidedTurns.length).toBeGreaterThan(0);
  });

  test("guided course brief falls back to edge HTTP when WebSocket is blocked", async ({
    page,
  }) => {
    serverState.mode = "ws_blocked";
    const mocks = await mockCommonRoutes(page, { chatEdgeEnabled: true });

    await page.addInitScript(() => {
      try {
        localStorage.setItem("duolify.guided.course_brief", "off");
      } catch {
        // ignore
      }
    });

    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto("/courses/create");

    const input = page.getByRole("textbox", {
      name: "Your answer",
    });
    await expect(input).toBeVisible();
    await input.fill("Quantum Physics");
    await page.getByRole("button", { name: "Send answer" }).click();

    await expect(
      page.getByText("Goal via Edge HTTP fallback", { exact: true }),
    ).toBeVisible();
    expect(mocks.guidedEdgeChannelCalls.length).toBeGreaterThan(0);
    expect(mocks.legacyGuidedCalls.length).toBe(0);
    expect(serverState.guidedTurns.length).toBeGreaterThan(0);
    expect(serverState.fallbackCalls.length).toBe(1);
    expect(serverState.fallbackCalls[0]).toEqual({
      from_transport: "websocket",
      to_transport: "sse",
      reason_code: "upgrade_failed",
    });
  });
});
