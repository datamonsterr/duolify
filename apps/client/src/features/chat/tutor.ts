import { AuthError, session } from "../accounts/session";
import { secureStore } from "../accounts/secure-store";
import type { WalletResponse } from "../credits/types";
import type { ActionQuote } from "../pricing/quotes";

/** A reference only: the API looks the lesson up itself and never trusts client-supplied lesson text. */
export type LessonRef = { lesson_id: string; slide_index: number };
export type ReplyClass = "short" | "long";
export type TutorQuote = Pick<
  ActionQuote,
  | "id"
  | "content_ceiling"
  | "pricing_version"
  | "daily_free_eligible"
  | "expires_at"
>;
export type TutorMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  status: "complete" | "interrupted" | "stopped";
  created_at: string;
};
export type ThreadResponse = {
  thread: { id: string } | null;
  messages: TutorMessage[];
};
export type SavedNote = { id: string; status: string; truncated: boolean };
export type SendInput = {
  quote_id: string;
  idempotency_key: string;
  class: ReplyClass;
  message: string;
  thread_id?: string;
  lesson?: LessonRef;
};
export type SendHandlers = {
  /** The event stream is open: the turn is accepted and a reply is coming. */
  onOpen: () => void;
  onThread: (threadId: string) => void;
  onDelta: (text: string) => void;
};
export type SendDone = {
  message_id: string;
  thread_id: string;
  replayed: boolean;
};
export interface TutorService {
  wallet(): Promise<WalletResponse>;
  thread(courseId: string): Promise<ThreadResponse>;
  /** `approveLargeContext` is the Learner's explicit approval of a context above the standard bound. */
  quote(
    courseId: string,
    cls: ReplyClass,
    message: string,
    approveLargeContext?: boolean,
    lesson?: LessonRef,
  ): Promise<TutorQuote>;
  accept(quoteId: string): Promise<unknown>;
  send(
    courseId: string,
    input: SendInput,
    handlers: SendHandlers,
    signal?: AbortSignal,
  ): Promise<SendDone>;
  remove(threadId: string): Promise<void>;
  /** Saves one of the Learner's replies as a raw Quick note; repeating it returns the same note. */
  saveReply(messageId: string, lessonId?: string): Promise<SavedNote>;
  /** Ask the server to end this send's reply; the stored prefix is settled once. */
  stop(courseId: string, key: string): Promise<void>;
  /** The owner's stored turn, including a partial reply after a dropped connection. */
  turn(courseId: string, key: string): Promise<StoredTurn | null>;
}
export type StoredTurn = {
  state: string;
  end_reason?: string;
  thread_id?: string;
  message: {
    id: string;
    content: string;
    status: TutorMessage["status"];
  } | null;
};

export type TutorErrorDetails = {
  next_grant_at?: string;
  required_tokens?: number;
  standard_limit_tokens?: number;
  prompt_limit_tokens?: number;
};

/** A refusal returned as JSON before any event was streamed. */
export class TutorApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    public details: TutorErrorDetails = {},
  ) {
    super(code);
  }
}
/** The stream ended with an `error` event after it had opened. */
export class TutorStreamError extends Error {
  constructor(
    public code: string,
    public interrupted: boolean,
    public messageId?: string,
    public retryable = false,
  ) {
    super(code);
  }
}

export type SseEvent = { event: string; data: unknown };
function frame(block: string): SseEvent | undefined {
  let event = "message";
  const data: string[] = [];
  for (const line of block.split("\n")) {
    if (!line || line.startsWith(":")) continue;
    const colon = line.indexOf(":");
    const field = colon < 0 ? line : line.slice(0, colon);
    const value = colon < 0 ? "" : line.slice(colon + 1).replace(/^ /, "");
    if (field === "event") event = value;
    else if (field === "data") data.push(value);
  }
  if (!data.length) return undefined;
  try {
    return { event, data: JSON.parse(data.join("\n")) };
  } catch {
    return undefined;
  }
}
export async function* parseSse(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<SseEvent> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    for (;;) {
      const { value, done } = await reader.read();
      buffer += decoder.decode(value, { stream: !done });
      // A chunk may end between "\r" and "\n": hold that "\r" back.
      const held = !done && buffer.endsWith("\r") ? "\r" : "";
      buffer =
        buffer.slice(0, buffer.length - held.length).replace(/\r\n?/g, "\n") +
        held;
      let end = buffer.indexOf("\n\n");
      while (end >= 0) {
        const event = frame(buffer.slice(0, end));
        buffer = buffer.slice(end + 2);
        if (event) yield event;
        end = buffer.indexOf("\n\n");
      }
      if (done) break;
    }
    const last = frame(buffer);
    if (last) yield last;
  } finally {
    reader.releaseLock();
  }
}

const base = import.meta.env.VITE_API_URL || "http://localhost:8080";
const course = (id: string) => `/v1/courses/${encodeURIComponent(id)}/tutor`;
function asApiError(error: unknown): never {
  if (error instanceof AuthError) {
    const details =
      error.details && typeof error.details === "object"
        ? (error.details as { error?: string } & TutorErrorDetails)
        : {};
    throw new TutorApiError(error.status, details.error ?? error.message, {
      next_grant_at: details.next_grant_at,
      required_tokens: details.required_tokens,
      standard_limit_tokens: details.standard_limit_tokens,
      prompt_limit_tokens: details.prompt_limit_tokens,
    });
  }
  throw error;
}
async function call<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  try {
    return await session.request<T>(path, method, body);
  } catch (error) {
    return asApiError(error);
  }
}

export const tutorService: TutorService = {
  wallet: () => call("/v1/me/credits"),
  thread: (id) => call(`${course(id)}/thread`),
  quote: (id, cls, message, approveLargeContext, lesson) =>
    call(`${course(id)}/quote`, "POST", {
      class: cls,
      message,
      ...(approveLargeContext ? { approve_large_context: true } : {}),
      ...(lesson ? { lesson } : {}),
    }),
  accept: (quoteId) =>
    call(`/v1/quotes/${encodeURIComponent(quoteId)}/accept`, "POST", {}),
  stop: async (id, key) => {
    await call(
      `${course(id)}/turns/${encodeURIComponent(key)}/stop`,
      "POST",
      {},
    );
  },
  turn: async (id, key) => {
    try {
      return await call<StoredTurn>(
        `${course(id)}/turns/${encodeURIComponent(key)}`,
      );
    } catch {
      return null;
    }
  },
  saveReply: (messageId, lessonId) =>
    call(`/v1/tutor/messages/${encodeURIComponent(messageId)}/note`, "POST", {
      ...(lessonId ? { lesson_id: lessonId } : {}),
    }),
  remove: (threadId) =>
    call(`/v1/tutor/threads/${encodeURIComponent(threadId)}`, "DELETE"),
  async send(id, input, handlers, signal) {
    const tokens = await secureStore.read();
    const response = await fetch(`${base}${course(id)}/messages`, {
      method: "POST",
      signal,
      headers: {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
        Authorization: `Bearer ${tokens?.access_token ?? ""}`,
      },
      body: JSON.stringify(input),
    });
    const stream = response.headers
      .get("content-type")
      ?.includes("text/event-stream");
    if (!response.ok || !stream || !response.body) {
      let details: { error?: string } & TutorErrorDetails = {};
      try {
        details = await response.json();
      } catch {
        /* Proxy and offline errors carry no JSON body. */
      }
      throw new TutorApiError(
        response.ok ? 502 : response.status,
        details.error ?? "unavailable",
        details,
      );
    }
    handlers.onOpen();
    let done: SendDone | undefined;
    for await (const { event, data } of parseSse(response.body)) {
      const payload = (data ?? {}) as Record<string, unknown>;
      if (event === "thread") handlers.onThread(String(payload.thread_id));
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
    // A stream that closes without `done` was cut: the prefix is an interrupted reply.
    if (!done) throw new TutorStreamError("stream_closed", true);
    return done;
  },
};
