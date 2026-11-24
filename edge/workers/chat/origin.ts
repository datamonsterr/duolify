import {
  maxBodyBytes,
  servicePath,
  serviceToken,
  type Operation,
  type Scope,
  type Transport,
} from "./protocol";

export interface OriginEnv {
  CHAT_API_ORIGIN: string;
  EDGE_SERVICE_SECRET: string;
}

export function chatError(status: number, error: string): Response {
  return Response.json(
    { error },
    { status, headers: { "Cache-Control": "no-store" } },
  );
}

export function apiURL(value: string): URL | null {
  try {
    const url = new URL(value);
    const local = url.hostname === "127.0.0.1" || url.hostname === "localhost";
    if (
      (url.protocol !== "https:" && !(local && url.protocol === "http:")) ||
      url.username ||
      url.password ||
      url.pathname !== "/" ||
      url.search ||
      url.hash
    ) {
      return null;
    }
    return url;
  } catch {
    return null;
  }
}

export async function readBody(
  request: Request,
): Promise<Uint8Array<ArrayBuffer> | null> {
  if (!request.body) return new Uint8Array(0);
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > maxBodyBytes) {
        await reader.cancel();
        return null;
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return bytes;
}

export async function callOrigin(
  env: OriginEnv,
  operation: Operation,
  body: Uint8Array<ArrayBuffer>,
  transport: Transport,
  scope?: Scope,
  signal?: AbortSignal,
): Promise<Response> {
  const origin = apiURL(env.CHAT_API_ORIGIN);
  if (
    !origin ||
    !env.EDGE_SERVICE_SECRET ||
    env.EDGE_SERVICE_SECRET.trim().length < 32
  ) {
    return chatError(503, "edge_unavailable");
  }
  const path = servicePath + operation.path;
  const token = await serviceToken(
    env.EDGE_SERVICE_SECRET,
    operation.method,
    path,
    body,
    transport,
    scope,
  );
  try {
    const response = await fetch(new URL(path, origin), {
      method: operation.method,
      headers: {
        Authorization: "Bearer " + token,
        "Content-Type": "application/json",
        Accept: "application/json, text/event-stream",
      },
      body: operation.method === "POST" ? body : undefined,
      redirect: "manual",
      signal,
    });
    if (response.status >= 300 && response.status < 400) {
      await response.body?.cancel().catch(() => undefined);
      return chatError(503, "edge_unavailable");
    }
    return response;
  } catch {
    return chatError(503, "edge_unavailable");
  }
}

export function isSSE(
  response: Response,
): response is Response & { body: ReadableStream<Uint8Array> } {
  return (
    response.ok &&
    response.body !== null &&
    response.headers.get("Content-Type")?.split(";")[0].trim() ===
      "text/event-stream"
  );
}

export async function readResult(response: Response): Promise<unknown> {
  if (!response.body) return null;
  if (
    response.headers.get("Content-Type")?.split(";")[0].trim() !==
    "application/json"
  ) {
    await response.body.cancel().catch(() => undefined);
    return { error: "edge_unavailable" };
  }
  try {
    return await response.json();
  } catch {
    return { error: "edge_unavailable" };
  }
}
