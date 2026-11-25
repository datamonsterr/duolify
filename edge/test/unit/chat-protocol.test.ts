import { describe, expect, it } from "vitest";
import {
  parseCommand,
  parseSSE,
  route,
  serviceToken,
} from "../../workers/chat/protocol";

describe("chat transport contract", () => {
  it("binds service credentials to the method, stable route and exact body", async () => {
    const secret = "edge-test-shared-secret-only-123456";
    const body = new Uint8Array(
      new TextEncoder().encode('{"idempotency_key":"same-turn"}'),
    );
    const now = 1_780_000_000_000;
    const token = await serviceToken(
      secret,
      "POST",
      "/internal/edge/chat/messages",
      body,
      "websocket",
      undefined,
      now,
    );
    const [payload, signature] = token.split(".");
    const decode = (part: string) =>
      Uint8Array.from(
        atob(part.replaceAll("-", "+").replaceAll("_", "/")),
        (byte) => byte.charCodeAt(0),
      );
    expect(JSON.parse(new TextDecoder().decode(decode(payload)))).toEqual({
      exp: 1780000030,
      method: "POST",
      path: "/internal/edge/chat/messages",
      body_sha256: Array.from(
        new Uint8Array(await crypto.subtle.digest("SHA-256", body)),
        (b) => b.toString(16).padStart(2, "0"),
      ).join(""),
      transport: "websocket",
    });
    const key = await crypto.subtle.importKey(
      "raw",
      new TextEncoder().encode(secret),
      { name: "HMAC", hash: "SHA-256" },
      false,
      ["verify"],
    );
    expect(
      await crypto.subtle.verify(
        "HMAC",
        key,
        decode(signature),
        new TextEncoder().encode("duolify-edge-chat-v1." + payload),
      ),
    ).toBe(true);
  });

  it("maps split SSE events, heartbeats and UTF-8 without inventing terminal events", async () => {
    const input = new TextEncoder().encode(
      ': keepalive\n\nevent: delta\ndata: {"text":"écho"}\n\n',
    );
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        for (const byte of input) controller.enqueue(Uint8Array.of(byte));
        controller.close();
      },
    });
    const frames = [];
    for await (const event of parseSSE(stream)) frames.push(event);
    expect(frames).toEqual([
      { event: "heartbeat", data: null },
      { event: "delta", seq: 1, data: { text: "écho" } },
    ]);
  });

  it("uses bounded turn keys and rejects unsupported operation shapes", () => {
    expect(
      parseCommand(JSON.stringify({ id: "req", op: "stop", key: "same-turn" })),
    ).toMatchObject({
      id: "req",
      operation: { method: "POST", path: "/turns/same-turn/stop" },
    });
    expect(route("GET", "/turns/" + "a".repeat(129))).toBeNull();
    expect(route("GET", "/turns/%2F")).toBeNull();
    expect(
      parseCommand('{"id":"r","op":"send","body":{"quote_id":"q"}}'),
    ).toBeNull();
    expect(parseCommand('{"id":"r","op":"balance"}')).toBeNull();
  });
});
