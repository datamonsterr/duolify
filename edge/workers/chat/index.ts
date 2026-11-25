import {
  Agent,
  routeAgentRequest,
  type Connection,
  type ConnectionContext,
  type WSMessage,
} from "agents";
import {
  callOrigin,
  chatError,
  isSSE,
  readBody,
  readResult,
  type OriginEnv,
} from "./origin";
import {
  encodeText,
  liveScope,
  normalizeScope,
  parseCommand,
  parseSSE,
  publicPath,
  route,
  scopeHeader,
  sessionObjectName,
  subprotocol,
  type ChatFrame,
  type Scope,
} from "./protocol";

interface Env extends OriginEnv {
  ChatSession: DurableObjectNamespace<ChatSession>;
  CHAT_EDGE_ENABLED: string;
  API_ALLOWED_ORIGINS: string;
}

const unavailable = { code: "edge_unavailable", retryable: true };
const internalPrefix = "/agents/chat-session/";
const channelPattern = /^[\w-]{1,4096}\.[\w-]{1,128}$/;

function readScope(request: Request): Scope | null {
  try {
    const parsed: unknown = JSON.parse(
      request.headers.get(scopeHeader) ?? "null",
    );
    return liveScope(parsed) ? normalizeScope(parsed) : null;
  } catch {
    return null;
  }
}

async function* streamFrames(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<ChatFrame> {
  let seq = 0;
  try {
    for await (const frame of parseSSE(body)) {
      if ("seq" in frame) seq = frame.seq;
      yield frame;
      if (frame.event === "done" || frame.event === "error") return;
    }
  } catch {
    // The canonical turn is recoverable from Go after a failed origin stream.
  }
  yield { event: "error", data: unavailable, seq: seq + 1 };
}

function sseResponse(body: ReadableStream<Uint8Array>): Response {
  const frames = streamFrames(body);
  return new Response(
    new ReadableStream({
      async pull(controller) {
        const next = await frames.next();
        if (next.done) {
          controller.close();
          return;
        }
        const frame = next.value;
        const wire =
          frame.event === "heartbeat"
            ? ": keepalive\n\n"
            : "id: " +
              frame.seq +
              "\nevent: " +
              frame.event +
              "\ndata: " +
              JSON.stringify(frame.data) +
              "\n\n";
        controller.enqueue(encodeText(wire));
      },
      async cancel() {
        await frames.return(undefined);
      },
    }),
    {
      headers: {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-store",
        "X-Accel-Buffering": "no",
      },
    },
  );
}

export class ChatSession extends Agent<Env> {
  private readonly active = new Map<
    string,
    { connectionId: string; abort: AbortController }
  >();

  shouldSendProtocolMessages(): boolean {
    return false;
  }

  shouldConnectionBeReadonly(): boolean {
    return true;
  }

  onConnect(
    connection: Connection<Scope | null>,
    ctx: ConnectionContext,
  ): void {
    const scope = readScope(ctx.request);
    if (scope) connection.setState(scope);
    else connection.close(1008, "Unauthorized");
  }

  async onMessage(
    connection: Connection<Scope | null>,
    message: WSMessage,
  ): Promise<void> {
    const scope = connection.state;
    if (!liveScope(scope)) {
      connection.setState(null);
      connection.close(1008, "Channel expired");
      return;
    }
    const command = typeof message === "string" ? parseCommand(message) : null;
    if (!command) {
      connection.close(1008, "Invalid command");
      return;
    }
    const { id, operation, body } = command;
    const send = (data: object) => {
      if (connection.readyState === WebSocket.OPEN)
        connection.send(JSON.stringify({ id, ...data }));
    };
    const bytes = body ? encodeText(JSON.stringify(body)) : new Uint8Array(0);
    const turn = operation.path === "/messages" ? operation.key : undefined;
    if (turn && this.active.has(turn)) {
      send({
        event: "result",
        status: 409,
        data: { error: "turn_in_progress" },
      });
      return;
    }
    const abort = new AbortController();
    if (turn) this.active.set(turn, { connectionId: connection.id, abort });
    try {
      const response = await callOrigin(
        this.env,
        operation,
        bytes,
        "websocket",
        scope,
        abort.signal,
      );
      if (isSSE(response)) {
        for await (const frame of streamFrames(response.body)) send(frame);
        return;
      }
      if (
        operation.key &&
        operation.path.endsWith("/stop") &&
        response.status === 202
      ) {
        this.active.get(operation.key)?.abort.abort();
      }
      send({
        event: "result",
        status: response.status,
        data: await readResult(response),
      });
      if (response.status === 401) {
        connection.setState(null);
        connection.close(1008, "Unauthorized");
      }
    } finally {
      if (turn) this.active.delete(turn);
    }
  }

  onClose(connection: Connection): void {
    for (const { connectionId, abort } of this.active.values()) {
      if (connectionId === connection.id) abort.abort();
    }
  }

  async onRequest(request: Request): Promise<Response> {
    const scope = readScope(request);
    if (!scope) return chatError(401, "unauthorized");
    const url = new URL(request.url);
    const suffix = url.pathname.slice(internalPrefix.length + this.name.length);
    const operation = route(request.method, suffix);
    if (!operation || suffix === "/authorize")
      return chatError(404, "not_found");
    const body = await readBody(request);
    if (!body) return chatError(413, "invalid_request");
    const response = await callOrigin(
      this.env,
      operation,
      body,
      "sse",
      scope,
      request.signal,
    );
    if (isSSE(response)) return sseResponse(response.body);
    const data = await readResult(response);
    return data === null
      ? new Response(null, { status: response.status })
      : Response.json(data, { status: response.status });
  }
}

function cors(origin: string): Headers {
  return new Headers({
    "Access-Control-Allow-Origin": origin,
    "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
    "Access-Control-Allow-Headers":
      "Authorization, Content-Type, Last-Event-ID",
    "Cache-Control": "no-store",
    Vary: "Origin",
  });
}

function withCORS(response: Response, origin: string): Response {
  const headers = new Headers(response.headers);
  cors(origin).forEach((value, key) => headers.set(key, value));
  return new Response(response.body, { status: response.status, headers });
}

function channelCredential(request: Request, socket: boolean): string {
  if (socket) {
    const parts = request.headers
      .get("Sec-WebSocket-Protocol")
      ?.split(",")
      .map((part) => part.trim());
    return parts?.length === 2 && parts[0] === subprotocol ? parts[1] : "";
  }
  return (
    /^Bearer (\S+)$/.exec(request.headers.get("Authorization") ?? "")?.[1] ?? ""
  );
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (!url.pathname.startsWith(publicPath) || url.search)
      return chatError(404, "not_found");
    const suffix = url.pathname.slice(publicPath.length);
    const socket =
      request.headers.get("Upgrade")?.toLowerCase() === "websocket";
    const method =
      request.method === "OPTIONS"
        ? (request.headers.get("Access-Control-Request-Method") ?? "")
        : request.method;
    if (
      socket
        ? method !== "GET" || suffix !== ""
        : !route(method, suffix) || suffix === "/authorize"
    ) {
      return chatError(404, "not_found");
    }
    const origin = request.headers.get("Origin");
    if (
      !origin ||
      !env.API_ALLOWED_ORIGINS.split(",").some((item) => item.trim() === origin)
    ) {
      return chatError(403, "origin_not_allowed");
    }
    if (env.CHAT_EDGE_ENABLED !== "true")
      return withCORS(chatError(404, "edge_disabled"), origin);
    if (request.method === "OPTIONS")
      return new Response(null, { status: 204, headers: cors(origin) });
    const token = channelCredential(request, socket);
    if (!channelPattern.test(token))
      return withCORS(chatError(401, "unauthorized"), origin);
    const authBody = encodeText(JSON.stringify({ token, origin }));
    const authorized = await callOrigin(
      env,
      { method: "POST", path: "/authorize" },
      authBody,
      socket ? "websocket" : "sse",
    );
    const result = await readResult(authorized);
    if (!authorized.ok || !liveScope(result, origin)) {
      const status =
        authorized.status === 404 ? 404 : authorized.status >= 500 ? 503 : 401;
      const code =
        status === 404
          ? "not_found"
          : status === 503
            ? "edge_unavailable"
            : "unauthorized";
      return withCORS(chatError(status, code), origin);
    }
    const scope = normalizeScope(result);
    url.pathname = internalPrefix + (await sessionObjectName(scope)) + suffix;
    const headers = new Headers({ [scopeHeader]: JSON.stringify(scope) });
    if (socket) headers.set("Upgrade", "websocket");
    const internal = new Request(url, {
      method: request.method,
      body: request.body,
      headers,
      signal: request.signal,
    });
    try {
      const routed = await routeAgentRequest(internal, env);
      if (routed?.status === 101 && routed.webSocket) {
        return new Response(null, {
          status: 101,
          webSocket: routed.webSocket,
          headers: {
            "Sec-WebSocket-Protocol": subprotocol,
            "Cache-Control": "no-store",
          },
        });
      }
      return withCORS(routed ?? chatError(503, "edge_unavailable"), origin);
    } catch {
      return withCORS(chatError(503, "edge_unavailable"), origin);
    }
  },
} satisfies ExportedHandler<Env>;
