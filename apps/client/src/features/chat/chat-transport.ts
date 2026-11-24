import { session } from "../accounts/session";
import {
  isChatEdgeEnabled,
  fetchChatEdgeEnabled,
  useChatEdgeEnabled,
} from "../../lib/features";
import {
  trackChatTransportFallback,
  type ChatFallbackReasonCode,
} from "../../lib/analytics";
import {
  type TutorService,
  type SendInput,
  type SendHandlers,
  type SendDone,
  type StoredTurn,
  type ThreadResponse,
  type TutorPersona,
  type TutorErrorDetails,
  TutorApiError,
  TutorStreamError,
  parseSse,
  legacyTutorService,
} from "./tutor";

export { isChatEdgeEnabled, fetchChatEdgeEnabled, useChatEdgeEnabled };

export function getEdgeChatUrl(): string {
  if (
    typeof window !== "undefined" &&
    (window as unknown as { __EDGE_CHAT_URL__?: string }).__EDGE_CHAT_URL__
  ) {
    return (window as unknown as { __EDGE_CHAT_URL__?: string })
      .__EDGE_CHAT_URL__!;
  }
  const envUrl =
    import.meta.env.VITE_EDGE_CHAT_URL || import.meta.env.VITE_CHAT_EDGE_URL;
  if (envUrl) {
    return envUrl;
  }
  return import.meta.env.VITE_API_URL ?? "http://localhost:8080";
}

function toWebSocketUrl(httpUrl: string): string {
  const url = new URL(
    httpUrl,
    typeof window !== "undefined"
      ? window.location.href
      : "http://localhost:8080",
  );
  const protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${url.host}/v1/edge/chat`;
}

export function calculateBackoff(attempt: number): number {
  const base = 100 * Math.pow(2, attempt);
  const jitter = 0.75 + Math.random() * 0.5;
  return Math.min(3000, Math.round(base * jitter));
}

export class FrameDeduplicator {
  private highestSeq = 0;

  accept(seq?: number): boolean {
    if (seq === undefined) return true;
    if (seq <= this.highestSeq) return false;
    this.highestSeq = seq;
    return true;
  }

  reset(): void {
    this.highestSeq = 0;
  }
}

export function formatWsCommand(
  id: string,
  op: string,
  body?: unknown,
  key?: string,
): string {
  return JSON.stringify({
    id,
    op,
    ...(body !== undefined ? { body } : {}),
    ...(key !== undefined ? { key } : {}),
  });
}

export type ParsedWsMessage =
  | { type: "result"; id: string; status: number; data: unknown }
  | { type: "heartbeat"; id: string }
  | {
      type: "stream";
      id: string;
      event: "thread" | "tutor" | "delta" | "done" | "error";
      data: unknown;
      seq?: number;
    };

export function parseWsMessage(raw: string): ParsedWsMessage | null {
  try {
    const val = JSON.parse(raw);
    if (!val || typeof val !== "object" || typeof val.id !== "string") {
      return null;
    }
    const { id, event, status, data, seq } = val;
    if (event === "result") {
      return { type: "result", id, status: Number(status), data };
    }
    if (event === "heartbeat") {
      return { type: "heartbeat", id };
    }
    if (["thread", "tutor", "delta", "done", "error"].includes(event)) {
      return { type: "stream", id, event, data, seq };
    }
    return null;
  } catch {
    return null;
  }
}

export function createPrivacySafeError(
  code: string,
  rawDetails?: unknown,
): TutorApiError {
  void rawDetails;
  return new TutorApiError(500, code, {});
}

interface ChannelGrant {
  token: string;
  expires_at: number;
}

const tutorGrants = new Map<string, ChannelGrant>();
const guidedGrants = new Map<string, ChannelGrant>();

export function resetEdgeGrants(): void {
  tutorGrants.clear();
  guidedGrants.clear();
}

export async function getTutorEdgeChannel(
  courseId: string,
  threadId?: string,
): Promise<string> {
  const cacheKey = `${courseId}:${threadId ?? ""}`;
  const cached = tutorGrants.get(cacheKey);
  const now = Math.floor(Date.now() / 1000);
  if (cached && cached.expires_at > now + 15) {
    return cached.token;
  }

  const res = await session.request<ChannelGrant>(
    `/v1/courses/${encodeURIComponent(courseId)}/tutor/edge-channel`,
    "POST",
    { thread_id: threadId ?? "" },
  );
  tutorGrants.set(cacheKey, res);
  return res.token;
}

export async function getGuidedEdgeChannel(
  surface: "setup" | "course_brief",
): Promise<string> {
  const cached = guidedGrants.get(surface);
  const now = Math.floor(Date.now() / 1000);
  if (cached && cached.expires_at > now + 15) {
    return cached.token;
  }

  const res = await session.request<ChannelGrant>(
    `/v1/me/guided-chat/edge-channel`,
    "POST",
    { surface },
  );
  guidedGrants.set(surface, res);
  return res.token;
}

export async function notifyEdgeFallback(
  surface: "tutor" | "guided",
  from: "websocket" | "sse",
  to: "sse" | "engine",
  reason: ChatFallbackReasonCode,
  token?: string,
): Promise<void> {
  if (token) {
    const base = getEdgeChatUrl();
    try {
      await fetch(`${base}/v1/edge/chat/fallback`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          from_transport: from,
          to_transport: to,
          reason_code: reason,
        }),
      });
    } catch {
      // ignore network errors on fallback report
    }
  } else {
    trackChatTransportFallback({
      surface,
      from_transport: from,
      to_transport: to,
      reason_code: reason,
    });
  }
}

export async function sendGuidedTurn<
  TInput extends Record<string, unknown>,
  TOutput,
>(
  surface: "setup" | "course_brief",
  turn: TInput,
  legacyFallback: () => Promise<TOutput>,
): Promise<TOutput> {
  if (!isChatEdgeEnabled()) {
    return legacyFallback();
  }

  let token: string | undefined;
  try {
    token = await getGuidedEdgeChannel(surface);
  } catch {
    await notifyEdgeFallback(
      "guided",
      "websocket",
      "engine",
      "edge_unavailable",
    );
    return legacyFallback();
  }

  // 1. Try WebSocket
  try {
    const wsUrl = toWebSocketUrl(getEdgeChatUrl());
    const result = await new Promise<TOutput>((resolve, reject) => {
      let ws: WebSocket | null = null;
      let opened = false;
      const id =
        typeof crypto !== "undefined" && crypto.randomUUID
          ? crypto.randomUUID()
          : `g-${Date.now()}`;

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("ws_timeout"));
      }, 5000);

      const cleanup = () => {
        clearTimeout(timer);
        if (ws) {
          ws.onopen = null;
          ws.onmessage = null;
          ws.onerror = null;
          ws.onclose = null;
          try {
            ws.close();
          } catch {
            // ignore
          }
          ws = null;
        }
      };

      try {
        ws = new WebSocket(wsUrl, ["duolify.chat.v1", token!]);
      } catch (err) {
        cleanup();
        reject(err);
        return;
      }

      ws.onopen = () => {
        opened = true;
        try {
          ws?.send(JSON.stringify({ op: "guided", id, body: turn }));
        } catch (err) {
          cleanup();
          reject(err);
        }
      };

      ws.onmessage = (ev) => {
        try {
          const raw = typeof ev.data === "string" ? ev.data : "";
          const msg = JSON.parse(raw);
          if (msg.id === id || msg.event === "result") {
            cleanup();
            if (msg.status >= 200 && msg.status < 300) {
              resolve(msg.data as TOutput);
            } else {
              reject(new Error(msg.data?.error ?? "guided_error"));
            }
          }
        } catch (err) {
          cleanup();
          reject(err);
        }
      };

      ws.onerror = () => {
        cleanup();
        reject(new Error("ws_error"));
      };

      ws.onclose = () => {
        if (!opened) {
          cleanup();
          reject(new Error("ws_closed_before_open"));
        }
      };
    });

    return result;
  } catch {
    await notifyEdgeFallback(
      "guided",
      "websocket",
      "sse",
      "upgrade_failed",
      token,
    );
  }

  // 2. Try Edge HTTP / SSE fallback
  try {
    const base = getEdgeChatUrl();
    const res = await fetch(`${base}/v1/edge/chat/guided-turn`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(turn),
    });

    if (res.ok) {
      return (await res.json()) as TOutput;
    }
    await notifyEdgeFallback(
      "guided",
      "sse",
      "engine",
      "edge_unavailable",
      token,
    );
  } catch {
    await notifyEdgeFallback(
      "guided",
      "sse",
      "engine",
      "edge_unavailable",
      token,
    );
  }

  // 3. Fall back to Go legacy
  return legacyFallback();
}

export interface EdgeTutorServiceOptions {
  edgeBaseUrl?: string;
}

export function createEdgeTutorService(
  options: EdgeTutorServiceOptions = {},
  baseService?: TutorService,
): TutorService {
  void options;
  const legacy = () => baseService ?? legacyTutorService;

  async function sendOverWebSocket(
    _courseId: string,
    input: SendInput,
    handlers: SendHandlers,
    token: string,
    signal?: AbortSignal,
  ): Promise<SendDone> {
    const wsUrl = toWebSocketUrl(getEdgeChatUrl());

    return new Promise<SendDone>((resolve, reject) => {
      let ws: WebSocket | null = null;
      let opened = false;
      let deltasDelivered = 0;
      const reqId = crypto.randomUUID();
      const dedup = new FrameDeduplicator();

      const cleanup = () => {
        if (ws) {
          ws.onopen = null;
          ws.onmessage = null;
          ws.onerror = null;
          ws.onclose = null;
          try {
            if (
              ws.readyState === WebSocket.OPEN ||
              ws.readyState === WebSocket.CONNECTING
            ) {
              ws.close();
            }
          } catch {
            // ignore
          }
          ws = null;
        }
      };

      if (signal?.aborted) {
        reject(new DOMException("Aborted", "AbortError"));
        return;
      }

      const onAbort = () => {
        cleanup();
        reject(new DOMException("Aborted", "AbortError"));
      };

      signal?.addEventListener("abort", onAbort, { once: true });

      try {
        ws = new WebSocket(wsUrl, ["duolify.chat.v1", token]);
      } catch (err) {
        signal?.removeEventListener("abort", onAbort);
        reject(err);
        return;
      }

      ws.onopen = () => {
        if (signal?.aborted) {
          cleanup();
          reject(new DOMException("Aborted", "AbortError"));
          return;
        }
        const cmd = formatWsCommand(reqId, "send", input);
        ws?.send(cmd);
      };

      ws.onmessage = (event) => {
        if (typeof event.data !== "string") return;
        const msg = parseWsMessage(event.data);
        if (!msg || msg.id !== reqId) return;

        if (msg.type === "heartbeat") {
          return;
        }

        if (msg.type === "stream") {
          if (!dedup.accept(msg.seq)) return;

          if (!opened) {
            opened = true;
            handlers.onOpen();
          }

          if (msg.event === "thread") {
            const payload = msg.data as { thread_id: string };
            if (payload?.thread_id)
              handlers.onThread(String(payload.thread_id));
          } else if (msg.event === "tutor") {
            handlers.onTutor?.(msg.data as unknown as TutorPersona);
          } else if (msg.event === "delta") {
            const payload = msg.data as { text: string };
            const text = String(payload?.text ?? "");
            deltasDelivered += text.length;
            handlers.onDelta(text);
          } else if (msg.event === "done") {
            cleanup();
            signal?.removeEventListener("abort", onAbort);
            resolve(msg.data as SendDone);
          } else if (msg.event === "error") {
            cleanup();
            signal?.removeEventListener("abort", onAbort);
            const errData = (msg.data ?? {}) as {
              code?: string;
              interrupted?: boolean;
              message_id?: string;
              retryable?: boolean;
            };
            reject(
              new TutorStreamError(
                String(errData.code ?? "error"),
                errData.interrupted === true,
                typeof errData.message_id === "string"
                  ? errData.message_id
                  : undefined,
                errData.retryable === true,
              ),
            );
          }
        } else if (msg.type === "result") {
          if (msg.status >= 400) {
            cleanup();
            signal?.removeEventListener("abort", onAbort);
            const details = (msg.data ?? {}) as Record<string, unknown>;
            reject(
              new TutorApiError(
                msg.status,
                String(details.error ?? "unavailable"),
                details as TutorErrorDetails,
              ),
            );
          }
        }
      };

      ws.onerror = () => {
        if (deltasDelivered > 0) {
          cleanup();
          signal?.removeEventListener("abort", onAbort);
          reject(new TutorStreamError("stream_closed", true));
          return;
        }
        cleanup();
        signal?.removeEventListener("abort", onAbort);
        reject(new Error("websocket_error"));
      };

      ws.onclose = (ev) => {
        signal?.removeEventListener("abort", onAbort);
        if (ev.wasClean && opened) {
          reject(new TutorStreamError("stream_closed", true));
          return;
        }
        if (deltasDelivered > 0) {
          reject(new TutorStreamError("stream_closed", true));
          return;
        }
        reject(new Error("websocket_closed"));
      };
    });
  }

  async function sendOverEdgeSSE(
    _courseId: string,
    input: SendInput,
    handlers: SendHandlers,
    token: string,
    signal?: AbortSignal,
  ): Promise<SendDone> {
    const edgeBase = getEdgeChatUrl();

    const response = await fetch(`${edgeBase}/v1/edge/chat/messages`, {
      method: "POST",
      signal,
      headers: {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(input),
    });

    const isStream = response.headers
      .get("content-type")
      ?.includes("text/event-stream");
    if (!response.ok || !isStream || !response.body) {
      let details: { error?: string } = {};
      try {
        details = await response.json();
      } catch {
        // ignore
      }
      throw new TutorApiError(
        response.ok ? 502 : response.status,
        details.error ?? "edge_unavailable",
        details as TutorErrorDetails,
      );
    }

    handlers.onOpen();
    let done: SendDone | undefined;
    const dedup = new FrameDeduplicator();

    for await (const { event, data } of parseSse(response.body)) {
      const payload = (data ?? {}) as Record<string, unknown>;
      if (typeof payload.seq === "number" && !dedup.accept(payload.seq)) {
        continue;
      }
      if (event === "thread") handlers.onThread(String(payload.thread_id));
      else if (event === "tutor")
        handlers.onTutor?.(payload as unknown as TutorPersona);
      else if (event === "delta") handlers.onDelta(String(payload.text ?? ""));
      else if (event === "done") done = payload as unknown as SendDone;
      else if (event === "error")
        throw new TutorStreamError(
          String(payload.code ?? "error"),
          payload.interrupted === true,
          typeof payload.message_id === "string"
            ? payload.message_id
            : undefined,
          payload.retryable === true,
        );
    }

    if (!done) throw new TutorStreamError("stream_closed", true);
    return done;
  }

  return {
    wallet: () => legacy().wallet(),
    accept: (quoteId) => legacy().accept(quoteId),
    remove: (threadId) => legacy().remove(threadId),
    saveReply: (messageId, lessonId) => legacy().saveReply(messageId, lessonId),
    personas: (courseId, lessonId) => {
      const fn = legacy().personas;
      if (!fn) return Promise.reject(new Error("personas_not_supported"));
      return fn(courseId, lessonId);
    },

    thread: async (id) => {
      if (!isChatEdgeEnabled()) {
        return legacy().thread(id);
      }
      try {
        const token = await getTutorEdgeChannel(id);
        const edgeBase = getEdgeChatUrl();
        const res = await fetch(`${edgeBase}/v1/edge/chat/thread`, {
          headers: { Authorization: `Bearer ${token}` },
        });
        if (res.ok) {
          return (await res.json()) as ThreadResponse;
        }
      } catch {
        // fallback
      }
      return legacy().thread(id);
    },

    quote: async (
      courseId,
      cls,
      message,
      approveLargeContext,
      lesson,
      region,
    ) => {
      // Quotes can go via legacy or edge; legacy Go owns quote acceptance
      return legacy().quote(
        courseId,
        cls,
        message,
        approveLargeContext,
        lesson,
        region,
      );
    },

    stop: async (courseId, key) => {
      if (!isChatEdgeEnabled()) {
        return legacy().stop(courseId, key);
      }
      try {
        const token = await getTutorEdgeChannel(courseId);
        const edgeBase = getEdgeChatUrl();
        const res = await fetch(
          `${edgeBase}/v1/edge/chat/turns/${encodeURIComponent(key)}/stop`,
          {
            method: "POST",
            headers: { Authorization: `Bearer ${token}` },
          },
        );
        if (res.ok) return;
      } catch {
        // fallback to legacy
      }
      return legacy().stop(courseId, key);
    },

    turn: async (courseId, key) => {
      if (!isChatEdgeEnabled()) {
        return legacy().turn(courseId, key);
      }
      try {
        const token = await getTutorEdgeChannel(courseId);
        const edgeBase = getEdgeChatUrl();
        const res = await fetch(
          `${edgeBase}/v1/edge/chat/turns/${encodeURIComponent(key)}`,
          {
            headers: { Authorization: `Bearer ${token}` },
          },
        );
        if (res.ok) {
          return (await res.json()) as StoredTurn;
        }
      } catch {
        // fallback to legacy
      }
      return legacy().turn(courseId, key);
    },

    send: async (courseId, input, handlers, signal) => {
      // 1. If edge chat is not enabled, directly use legacy path
      if (!isChatEdgeEnabled()) {
        return legacy().send(courseId, input, handlers, signal);
      }

      let token: string | undefined;
      try {
        token = await getTutorEdgeChannel(courseId, input.thread_id);
      } catch {
        await notifyEdgeFallback(
          "tutor",
          "websocket",
          "engine",
          "edge_unavailable",
        );
        return legacy().send(courseId, input, handlers, signal);
      }

      // 2. Try Edge WebSocket
      try {
        return await sendOverWebSocket(
          courseId,
          input,
          handlers,
          token,
          signal,
        );
      } catch (wsErr) {
        // If aborted or if turn was already interrupted/streaming, throw
        if (
          signal?.aborted ||
          (wsErr instanceof DOMException && wsErr.name === "AbortError") ||
          (wsErr instanceof TutorStreamError && wsErr.interrupted)
        ) {
          throw wsErr;
        }

        // Emit fallback from WebSocket to Edge SSE
        await notifyEdgeFallback(
          "tutor",
          "websocket",
          "sse",
          "upgrade_failed",
          token,
        );
      }

      // 3. Try Edge SSE
      try {
        return await sendOverEdgeSSE(courseId, input, handlers, token, signal);
      } catch (sseErr) {
        if (
          signal?.aborted ||
          (sseErr instanceof DOMException && sseErr.name === "AbortError") ||
          (sseErr instanceof TutorStreamError && sseErr.interrupted)
        ) {
          throw sseErr;
        }

        // Emit fallback from Edge SSE to Go/Engine
        await notifyEdgeFallback(
          "tutor",
          "sse",
          "engine",
          "edge_unavailable",
          token,
        );
      }

      // 4. Fallback to legacy Go/engine path
      return legacy().send(courseId, input, handlers, signal);
    },
  };
}

export const edgeTutorService = createEdgeTutorService();
