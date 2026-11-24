export const publicPath = "/v1/edge/chat";
export const servicePath = "/internal/edge/chat";
export const subprotocol = "duolify.chat.v1";
export const scopeHeader = "x-duolify-chat-scope";
export const maxBodyBytes = 64 * 1024;

export type Transport = "websocket" | "sse";

export interface Scope {
  learner_id: string;
  session_id: string;
  course_id: string;
  thread_id: string;
  origin: string;
  expires_at: number;
  surface?: "tutor" | "setup" | "course_brief";
}

export interface Operation {
  method: "GET" | "POST";
  path: string;
  key?: string;
}

const encoder = new TextEncoder();

export function encodeText(text: string): Uint8Array<ArrayBuffer> {
  return encoder.encode(text) as Uint8Array<ArrayBuffer>;
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const keyPattern = /^(?!\.{1,2}$)[\w.~-]{1,128}$/;

function isKey(value: unknown): value is string {
  return typeof value === "string" && keyPattern.test(value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function liveScope(value: unknown, origin?: string): value is Scope {
  if (
    !isRecord(value) ||
    typeof value.learner_id !== "string" ||
    !uuid.test(value.learner_id) ||
    typeof value.session_id !== "string" ||
    !uuid.test(value.session_id) ||
    typeof value.origin !== "string" ||
    !value.origin ||
    (origin !== undefined && value.origin !== origin) ||
    typeof value.expires_at !== "number" ||
    !Number.isSafeInteger(value.expires_at) ||
    value.expires_at <= Math.floor(Date.now() / 1000)
  ) {
    return false;
  }
  if (value.surface === "setup" || value.surface === "course_brief") {
    return value.course_id === "" && value.thread_id === "";
  }
  return (
    (value.surface === undefined || value.surface === "tutor") &&
    typeof value.course_id === "string" &&
    uuid.test(value.course_id) &&
    typeof value.thread_id === "string" &&
    (value.thread_id === "" || uuid.test(value.thread_id))
  );
}

export function normalizeScope(value: Scope): Scope {
  const {
    learner_id,
    session_id,
    course_id,
    thread_id,
    origin,
    expires_at,
    surface,
  } = value;
  return {
    learner_id,
    session_id,
    course_id,
    thread_id,
    origin,
    expires_at,
    surface,
  };
}

export function route(method: string, path: string): Operation | null {
  if (
    method === "POST" &&
    ["/quote", "/messages", "/guided-turn", "/fallback", "/authorize"].includes(
      path,
    )
  ) {
    return { method, path };
  }
  if (method === "GET" && path === "/thread") return { method, path };
  const turn = /^\/turns\/([^/]+)(\/stop)?$/.exec(path);
  if (
    turn &&
    isKey(turn[1]) &&
    (turn[2] ? method === "POST" : method === "GET")
  ) {
    return { method: turn[2] ? "POST" : "GET", path, key: turn[1] };
  }
  return null;
}

export interface Command {
  id: string;
  operation: Operation;
  body?: Record<string, unknown>;
}

export function parseCommand(wire: string): Command | null {
  if (wire.length > maxBodyBytes || encoder.encode(wire).length > maxBodyBytes)
    return null;
  let value: unknown;
  try {
    value = JSON.parse(wire);
  } catch {
    return null;
  }
  if (!isRecord(value) || !isKey(value.id)) return null;
  const { id, key, body } = value;
  switch (value.op) {
    case "quote":
      return isRecord(body)
        ? { id, operation: { method: "POST", path: "/quote" }, body }
        : null;
    case "send":
      return isRecord(body) && isKey(body.idempotency_key)
        ? {
            id,
            operation: {
              method: "POST",
              path: "/messages",
              key: body.idempotency_key,
            },
            body,
          }
        : null;
    case "thread":
      return { id, operation: { method: "GET", path: "/thread" } };
    case "guided":
      return isRecord(body)
        ? { id, operation: { method: "POST", path: "/guided-turn" }, body }
        : null;
    case "recover":
    case "stop": {
      if (!isKey(key)) return null;
      const stop = value.op === "stop";
      return {
        id,
        operation: {
          method: stop ? "POST" : "GET",
          path: "/turns/" + key + (stop ? "/stop" : ""),
          key,
        },
      };
    }
    default:
      return null;
  }
}

function base64url(bytes: Uint8Array): string {
  return btoa(String.fromCharCode(...bytes))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/, "");
}

async function sha256(bytes: Uint8Array<ArrayBuffer>): Promise<string> {
  const hash = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return Array.from(hash, (byte) => byte.toString(16).padStart(2, "0")).join(
    "",
  );
}

export async function sessionObjectName(scope: Scope): Promise<string> {
  return sha256(encodeText(scope.learner_id + "/" + scope.session_id));
}

export async function serviceToken(
  secret: string,
  method: string,
  path: string,
  bytes: Uint8Array<ArrayBuffer>,
  transport: Transport,
  scope?: Scope,
  now = Date.now(),
): Promise<string> {
  const input = {
    exp: Math.floor(now / 1000) + 30,
    method,
    path,
    body_sha256: await sha256(bytes),
    transport,
    scope,
  };
  const payload = base64url(encoder.encode(JSON.stringify(input)));
  const key = await crypto.subtle.importKey(
    "raw",
    encoder.encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const signature = await crypto.subtle.sign(
    "HMAC",
    key,
    encoder.encode("duolify-edge-chat-v1." + payload),
  );
  return payload + "." + base64url(new Uint8Array(signature));
}

type TutorEventName = "thread" | "tutor" | "delta" | "done" | "error";
const eventNames = new Set<string>([
  "thread",
  "tutor",
  "delta",
  "done",
  "error",
]);

export type ChatFrame =
  | { event: "heartbeat"; data: null }
  | { event: TutorEventName; data: unknown; seq: number };

function frameFromBlock(block: string, seq: number): ChatFrame | null {
  const lines = block.split("\n");
  const event = lines
    .find((line) => line.startsWith("event:"))
    ?.slice(6)
    .trim();
  if (!event)
    return lines.some((line) => line.startsWith(":"))
      ? { event: "heartbeat", data: null }
      : null;
  if (!eventNames.has(event)) throw new Error("Unexpected Tutor event");
  const data = lines
    .filter((line) => line.startsWith("data:"))
    .map((line) => line.slice(5).trimStart())
    .join("\n");
  return { event: event as TutorEventName, data: JSON.parse(data), seq };
}

export async function* parseSSE(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<ChatFrame> {
  const reader = body.pipeThrough(new TextDecoderStream()).getReader();
  let buffer = "";
  let seq = 1;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer = (buffer + value).replaceAll("\r\n", "\n");
      let end = buffer.indexOf("\n\n");
      while (end >= 0) {
        const frame = frameFromBlock(buffer.slice(0, end), seq);
        buffer = buffer.slice(end + 2);
        if (frame) {
          if (frame.event !== "heartbeat") seq++;
          yield frame;
        }
        end = buffer.indexOf("\n\n");
      }
      if (buffer.length > maxBodyBytes) throw new Error("Oversize Tutor event");
    }
    if (buffer.trim()) throw new Error("Incomplete Tutor event");
  } finally {
    await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}
