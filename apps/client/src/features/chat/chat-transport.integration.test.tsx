import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "../../test/setup";
import { secureStore } from "../accounts/secure-store";
import {
  createEdgeTutorService,
  sendGuidedTurn,
  resetEdgeGrants,
} from "./chat-transport";
import { briefApi, emptyBrief } from "../courses/brief";
import * as analytics from "../../lib/analytics";

describe("CF-06 client chat transport integration", () => {
  const courseId = "6f1d2b9e-0c3a-4b7e-9d1a-2f4c8e5a7b01";

  beforeEach(async () => {
    resetEdgeGrants();
    delete (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__;
    delete (window as unknown as Record<string, unknown>).__EDGE_CHAT_URL__;
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    await secureStore.write({
      access_token: "test.access.token",
      refresh_token: "test.refresh.token",
      expires_in: 3600,
    });
  });

  afterEach(async () => {
    resetEdgeGrants();
    delete (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__;
    delete (window as unknown as Record<string, unknown>).__EDGE_CHAT_URL__;
    vi.unstubAllGlobals();
    await secureStore.write(null);
  });

  it("selects WebSocket transport when chat_edge_enabled is true", async () => {
    (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__ = true;
    (window as unknown as Record<string, unknown>).__EDGE_CHAT_URL__ =
      "http://127.0.0.1:4192";

    const trackFallbackSpy = vi.spyOn(analytics, "trackChatTransportFallback");

    server.use(
      http.post("*/v1/courses/:id/tutor/edge-channel", () => {
        return HttpResponse.json({
          token: "fake.jwt.token",
          expires_at: Math.floor(Date.now() / 1000) + 300,
        });
      }),
    );

    // Mock global WebSocket
    class MockWs {
      static instances: MockWs[] = [];
      onopen: (() => void) | null = null;
      onmessage: ((ev: { data: string }) => void) | null = null;
      onerror: ((ev: Event) => void) | null = null;
      onclose: ((ev: { wasClean?: boolean }) => void) | null = null;
      readyState = 1;

      constructor(
        public url: string,
        public protocols: string[],
      ) {
        MockWs.instances.push(this);
        setTimeout(() => {
          this.onopen?.();
        }, 10);
      }

      send(data: string) {
        const msg = JSON.parse(data);
        if (msg.op === "send") {
          setTimeout(() => {
            this.onmessage?.({
              data: JSON.stringify({
                id: msg.id,
                event: "thread",
                data: { thread_id: "t1" },
                seq: 1,
              }),
            });
            this.onmessage?.({
              data: JSON.stringify({
                id: msg.id,
                event: "delta",
                data: { text: "Hello WS" },
                seq: 2,
              }),
            });
            this.onmessage?.({
              data: JSON.stringify({
                id: msg.id,
                event: "done",
                data: { message_id: "m1", thread_id: "t1", replayed: false },
                seq: 3,
              }),
            });
          }, 20);
        }
      }

      close() {
        this.readyState = 3;
        this.onclose?.({ wasClean: true });
      }
    }

    vi.stubGlobal("WebSocket", MockWs);

    const service = createEdgeTutorService();
    const handlers = {
      onOpen: vi.fn(),
      onThread: vi.fn(),
      onDelta: vi.fn(),
    };

    const done = await service.send(
      courseId,
      {
        quote_id: "q1",
        idempotency_key: "key-1",
        class: "short",
        message: "Hello",
      },
      handlers,
    );

    expect(done.message_id).toBe("m1");
    expect(handlers.onOpen).toHaveBeenCalled();
    expect(handlers.onDelta).toHaveBeenCalledWith("Hello WS");
    expect(trackFallbackSpy).not.toHaveBeenCalled();
  });

  it("falls back to Edge SSE when WebSocket connection fails and emits fallback telemetry once", async () => {
    (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__ = true;
    (window as unknown as Record<string, unknown>).__EDGE_CHAT_URL__ =
      "http://127.0.0.1:4192";

    const trackFallbackSpy = vi.spyOn(analytics, "trackChatTransportFallback");

    server.use(
      http.post("*/v1/courses/:id/tutor/edge-channel", () => {
        return HttpResponse.json({
          token: "fake.jwt.token",
          expires_at: Math.floor(Date.now() / 1000) + 300,
        });
      }),
      http.post("*/v1/edge/chat/fallback", () => {
        return new HttpResponse(null, { status: 204 });
      }),
      http.post("http://127.0.0.1:4192/v1/edge/chat/messages", () => {
        const stream = new ReadableStream({
          start(controller) {
            const encoder = new TextEncoder();
            controller.enqueue(
              encoder.encode('event: thread\ndata: {"thread_id":"t-sse"}\n\n'),
            );
            controller.enqueue(
              encoder.encode(
                'event: delta\ndata: {"text":"Hello Edge SSE"}\n\n',
              ),
            );
            controller.enqueue(
              encoder.encode(
                'event: done\ndata: {"message_id":"m-sse","thread_id":"t-sse","replayed":false}\n\n',
              ),
            );
            controller.close();
          },
        });
        return new HttpResponse(stream, {
          headers: { "Content-Type": "text/event-stream" },
        });
      }),
    );

    const serverFallbackCalls: Array<{
      from_transport: string;
      to_transport: string;
      reason_code: string;
    }> = [];
    server.use(
      http.post("*/v1/courses/:id/tutor/edge-channel", () => {
        return HttpResponse.json({
          token: "fake.jwt.token",
          expires_at: Math.floor(Date.now() / 1000) + 300,
        });
      }),
      http.post(
        "http://127.0.0.1:4192/v1/edge/chat/fallback",
        async ({ request }) => {
          serverFallbackCalls.push(
            (await request.json()) as {
              from_transport: string;
              to_transport: string;
              reason_code: string;
            },
          );
          return new HttpResponse(null, { status: 204 });
        },
      ),
    );

    class FailingWs {
      onopen: (() => void) | null = null;
      onerror: ((ev: Event) => void) | null = null;
      onclose: ((ev: { wasClean?: boolean }) => void) | null = null;
      readyState = 3;
      constructor() {
        setTimeout(() => {
          this.onerror?.(new Event("error"));
        }, 10);
      }
      close() {}
    }
    vi.stubGlobal("WebSocket", FailingWs);

    const service = createEdgeTutorService();
    const handlers = {
      onOpen: vi.fn(),
      onThread: vi.fn(),
      onDelta: vi.fn(),
    };

    const done = await service.send(
      courseId,
      {
        quote_id: "q1",
        idempotency_key: "key-1",
        class: "short",
        message: "Hello",
      },
      handlers,
    );

    expect(done.message_id).toBe("m-sse");
    expect(handlers.onDelta).toHaveBeenCalledWith("Hello Edge SSE");
    // Client wrapper should NOT be called when token exists (single emission via server)
    expect(trackFallbackSpy).not.toHaveBeenCalled();
    expect(serverFallbackCalls).toHaveLength(1);
    expect(serverFallbackCalls[0]).toEqual({
      from_transport: "websocket",
      to_transport: "sse",
      reason_code: "upgrade_failed",
    });
  });

  it("falls back to legacy Go/engine path when edge channel grant fails (emits client wrapper)", async () => {
    (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__ = true;
    (window as unknown as Record<string, unknown>).__EDGE_CHAT_URL__ =
      "http://127.0.0.1:4192";

    const trackFallbackSpy = vi.spyOn(analytics, "trackChatTransportFallback");

    server.use(
      http.post("*/v1/courses/:id/tutor/edge-channel", () => {
        return new HttpResponse(null, { status: 503 });
      }),
      http.post("*/v1/courses/:id/tutor/messages", () => {
        const stream = new ReadableStream({
          start(controller) {
            const encoder = new TextEncoder();
            controller.enqueue(
              encoder.encode(
                'event: thread\ndata: {"thread_id":"t-legacy"}\n\n',
              ),
            );
            controller.enqueue(
              encoder.encode(
                'event: delta\ndata: {"text":"Hello Legacy SSE"}\n\n',
              ),
            );
            controller.enqueue(
              encoder.encode(
                'event: done\ndata: {"message_id":"m-legacy","thread_id":"t-legacy","replayed":false}\n\n',
              ),
            );
            controller.close();
          },
        });
        return new HttpResponse(stream, {
          headers: { "Content-Type": "text/event-stream" },
        });
      }),
    );

    const service = createEdgeTutorService();
    const handlers = {
      onOpen: vi.fn(),
      onThread: vi.fn(),
      onDelta: vi.fn(),
    };

    const done = await service.send(
      courseId,
      {
        quote_id: "q1",
        idempotency_key: "key-2",
        class: "short",
        message: "Hello legacy",
      },
      handlers,
    );

    expect(done.message_id).toBe("m-legacy");
    expect(handlers.onDelta).toHaveBeenCalledWith("Hello Legacy SSE");
    expect(trackFallbackSpy).toHaveBeenCalledTimes(1);
    expect(trackFallbackSpy).toHaveBeenCalledWith(
      expect.objectContaining({
        surface: "tutor",
        from_transport: "websocket",
        to_transport: "engine",
        reason_code: "edge_unavailable",
      }),
    );
  });

  it("uses legacy Go path directly when chat_edge_enabled is false without calling edge or emitting fallback", async () => {
    (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__ =
      false;

    const trackFallbackSpy = vi.spyOn(analytics, "trackChatTransportFallback");

    server.use(
      http.post("*/v1/courses/:id/tutor/messages", () => {
        const stream = new ReadableStream({
          start(controller) {
            const encoder = new TextEncoder();
            controller.enqueue(
              encoder.encode(
                'event: thread\ndata: {"thread_id":"t-legacy2"}\n\n',
              ),
            );
            controller.enqueue(
              encoder.encode(
                'event: delta\ndata: {"text":"Direct legacy"}\n\n',
              ),
            );
            controller.enqueue(
              encoder.encode(
                'event: done\ndata: {"message_id":"m-legacy2","thread_id":"t-legacy2","replayed":false}\n\n',
              ),
            );
            controller.close();
          },
        });
        return new HttpResponse(stream, {
          headers: { "Content-Type": "text/event-stream" },
        });
      }),
    );

    const service = createEdgeTutorService();
    const handlers = {
      onOpen: vi.fn(),
      onThread: vi.fn(),
      onDelta: vi.fn(),
    };

    const done = await service.send(
      courseId,
      {
        quote_id: "q1",
        idempotency_key: "key-3",
        class: "short",
        message: "Flag false send",
      },
      handlers,
    );

    expect(done.message_id).toBe("m-legacy2");
    expect(handlers.onDelta).toHaveBeenCalledWith("Direct legacy");
    expect(trackFallbackSpy).not.toHaveBeenCalled();
  });

  it("routes guided turn over edge WebSocket and handles fallback", async () => {
    (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__ = true;
    (window as unknown as Record<string, unknown>).__EDGE_CHAT_URL__ =
      "http://127.0.0.1:4192";

    server.use(
      http.post("*/v1/me/guided-chat/edge-channel", () => {
        return HttpResponse.json({
          token: "guided.test.token",
          expires_at: Math.floor(Date.now() / 1000) + 300,
        });
      }),
    );

    class MockWsGuided {
      static instances: MockWsGuided[] = [];
      onopen: (() => void) | null = null;
      onmessage: ((ev: { data: string }) => void) | null = null;
      onerror: ((ev: Event) => void) | null = null;
      onclose: ((ev: { wasClean?: boolean }) => void) | null = null;
      readyState = 1;

      constructor(
        public url: string,
        public protocols: string[],
      ) {
        MockWsGuided.instances.push(this);
        setTimeout(() => {
          this.onopen?.();
        }, 10);
      }

      send(data: string) {
        const msg = JSON.parse(data);
        if (msg.op === "guided") {
          setTimeout(() => {
            this.onmessage?.({
              data: JSON.stringify({
                id: msg.id,
                event: "result",
                status: 200,
                data: {
                  next_step: "goal",
                  message: "What is your goal?",
                  brief: { topic: "Science" },
                },
              }),
            });
          }, 20);
        }
      }

      close() {
        this.readyState = 3;
        this.onclose?.({ wasClean: true });
      }
    }

    vi.stubGlobal("WebSocket", MockWsGuided);

    const fallback = vi.fn();
    const result = await sendGuidedTurn(
      "course_brief",
      { current_step: "topic", answer: "Science" },
      fallback,
    );

    expect(result).toEqual({
      next_step: "goal",
      message: "What is your goal?",
      brief: { topic: "Science" },
    });
    expect(fallback).not.toHaveBeenCalled();
  });

  it("routes briefApi.chat through edge WebSocket when enabled", async () => {
    (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__ = true;
    (window as unknown as Record<string, unknown>).__EDGE_CHAT_URL__ =
      "http://127.0.0.1:4192";

    server.use(
      http.post("*/v1/me/guided-chat/edge-channel", () => {
        return HttpResponse.json({
          token: "guided.brief.token",
          expires_at: Math.floor(Date.now() / 1000) + 300,
        });
      }),
    );

    class MockWsBrief {
      onopen: (() => void) | null = null;
      onmessage: ((ev: { data: string }) => void) | null = null;
      onerror: ((ev: Event) => void) | null = null;
      onclose: ((ev: { wasClean?: boolean }) => void) | null = null;
      readyState = 1;

      constructor(
        public url: string,
        public protocols: string[],
      ) {
        setTimeout(() => {
          this.onopen?.();
        }, 10);
      }

      send(data: string) {
        const msg = JSON.parse(data);
        if (msg.op === "guided") {
          setTimeout(() => {
            this.onmessage?.({
              data: JSON.stringify({
                id: msg.id,
                event: "result",
                status: 200,
                data: {
                  next_step: "goal",
                  message: "Tell me your goal",
                  brief: { ...emptyBrief(), topic: "Rust" },
                  is_follow_up: false,
                },
              }),
            });
          }, 20);
        }
      }

      close() {
        this.readyState = 3;
      }
    }

    vi.stubGlobal("WebSocket", MockWsBrief);

    const result = await briefApi.chat({
      brief: emptyBrief(),
      current_step: "topic",
      answer: "Rust",
      skipped: false,
      is_follow_up: false,
    });

    expect(result.next_step).toBe("goal");
    expect(result.message).toBe("Tell me your goal");
  });

  it("guided chat falls back to Go legacy when WS and Edge HTTP fail", async () => {
    (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__ = true;
    (window as unknown as Record<string, unknown>).__EDGE_CHAT_URL__ =
      "http://127.0.0.1:4192";

    const serverFallbackCalls: Array<{
      from_transport: string;
      to_transport: string;
      reason_code: string;
    }> = [];
    server.use(
      http.post("*/v1/me/guided-chat/edge-channel", () => {
        return HttpResponse.json({
          token: "guided.fallback.token",
          expires_at: Math.floor(Date.now() / 1000) + 300,
        });
      }),
      http.post(
        "http://127.0.0.1:4192/v1/edge/chat/fallback",
        async ({ request }) => {
          serverFallbackCalls.push(
            (await request.json()) as {
              from_transport: string;
              to_transport: string;
              reason_code: string;
            },
          );
          return new HttpResponse(null, { status: 204 });
        },
      ),
      http.post("http://127.0.0.1:4192/v1/edge/chat/guided-turn", () => {
        return new HttpResponse(null, { status: 503 });
      }),
    );

    class FailingWsGuided {
      onopen: (() => void) | null = null;
      onerror: ((ev: Event) => void) | null = null;
      onclose: ((ev: { wasClean?: boolean }) => void) | null = null;
      readyState = 3;
      constructor() {
        setTimeout(() => {
          this.onerror?.(new Event("error"));
        }, 10);
      }
      close() {}
    }
    vi.stubGlobal("WebSocket", FailingWsGuided);

    const legacyFallback = vi
      .fn<
        () => Promise<{ next_step: string; message: string; brief: unknown }>
      >()
      .mockResolvedValue({
        next_step: "done",
        message: "From legacy Go",
        brief: emptyBrief(),
      });

    const result = await sendGuidedTurn(
      "course_brief",
      { current_step: "topic", answer: "Math" },
      legacyFallback,
    );

    expect(result.next_step).toBe("done");
    expect(legacyFallback).toHaveBeenCalledTimes(1);
    expect(serverFallbackCalls).toHaveLength(2);
    expect(serverFallbackCalls[0]).toEqual({
      from_transport: "websocket",
      to_transport: "sse",
      reason_code: "upgrade_failed",
    });
    expect(serverFallbackCalls[1]).toEqual({
      from_transport: "sse",
      to_transport: "engine",
      reason_code: "edge_unavailable",
    });
  });
});
