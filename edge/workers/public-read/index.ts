import {
  cacheKey,
  edgeHeaders,
  freshness,
  isEnabled,
  isMarketplace,
  marketplaceTTL,
  publicReadMode,
  publicReadSurface,
  type Surface,
  type Freshness,
} from "./policy";

import { readMarketplace, type MarketplaceConfig } from "./marketplace";

interface Env extends MarketplaceConfig {
  MARKETPLACE_DB?: Hyperdrive;
  MARKETPLACE_RATE_LIMIT?: RateLimit;
  PUBLIC_READ_MODE?: string;
  PUBLIC_READ_ORIGIN: string;
  PUBLIC_READ_ORIGIN_SECRET?: string;
  PUBLIC_READ_EDGE_ENABLED?: string;
  MARKETPLACE_ENABLED?: string;
  MARKETPLACE_CACHE_TTL_SECONDS?: string;
  API_ALLOWED_ORIGINS?: string;
}

const internalExpiry = "X-Duolify-Edge-Expires-At";
const internalPolicy = "X-Duolify-Edge-Public-Cache-Control";
const internalHeaders = [
  ...Object.values(edgeHeaders),
  internalExpiry,
  internalPolicy,
];

function configuredOrigin(raw: string): URL | undefined {
  try {
    const url = new URL(raw);
    const local = ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname);
    if (url.protocol !== "https:" && !(local && url.protocol === "http:"))
      return;
    if (
      url.username ||
      url.password ||
      url.pathname !== "/" ||
      url.search ||
      url.hash
    )
      return;
    return url;
  } catch {
    return;
  }
}

const builtInOrigins = new Set([
  "tauri://localhost",
  "http://tauri.localhost",
  "https://tauri.localhost",
]);

function allowedOrigin(origin: string | null, env: Env): boolean {
  if (!origin || builtInOrigins.has(origin)) return true;
  return (env.API_ALLOWED_ORIGINS ?? "")
    .split(",")
    .some((value) => value.trim() === origin);
}

function responseHeaders(origin: string | null, surface?: Surface): Headers {
  const headers = new Headers({
    "Cache-Control": "no-store",
    "Content-Type": "application/json",
    "Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
    "Cross-Origin-Resource-Policy": "same-site",
    "Referrer-Policy": "no-referrer",
    "Strict-Transport-Security": "max-age=31536000; includeSubDomains",
    Vary: "Origin",
    "X-Content-Type-Options": "nosniff",
    "X-Request-ID": crypto.randomUUID(),
  });
  if (origin) {
    headers.set("Access-Control-Allow-Origin", origin);
    headers.set(
      "Access-Control-Allow-Methods",
      "GET, POST, PUT, PATCH, DELETE, OPTIONS",
    );
    headers.set(
      "Access-Control-Allow-Headers",
      "Authorization, Content-Type, X-Request-ID, X-Quote-ID, X-App-Version, X-Platform, If-None-Match",
    );
    headers.set(
      "Access-Control-Expose-Headers",
      "X-Request-ID, ETag, Cache-Control",
    );
  }
  if (surface && surface !== "shared_demo")
    headers.set("X-Robots-Tag", "noindex");
  return headers;
}

function publicResponse(
  request: Request,
  env: Env,
  surface: Surface | undefined,
  status: number,
  body: object,
): Response {
  const origin = request.headers.get("Origin");
  if (!allowedOrigin(origin, env)) {
    return new Response('{"error":"origin not allowed"}\n', {
      status: 403,
      headers: responseHeaders(null),
    });
  }
  return new Response(JSON.stringify(body) + "\n", {
    status,
    headers: responseHeaders(origin, surface),
  });
}

function publicHeaders(headers: Headers): Headers {
  const copy = new Headers(headers);
  for (const name of internalHeaders) copy.delete(name);
  return copy;
}

async function readOrigin(
  request: Request,
  origin: URL,
  surface: Surface | undefined,
  secret?: string,
  eligible = false,
): Promise<Response> {
  const url = new URL(request.url);
  const target = new URL(origin);
  target.pathname = url.pathname;
  target.search = url.search;
  const headers = publicHeaders(request.headers);
  headers.delete("host");
  if (surface && secret) {
    headers.set(edgeHeaders.secret, secret);
    const clientIP = headers.get("CF-Connecting-IP");
    if (clientIP) headers.set(edgeHeaders.clientIP, clientIP);
    if (eligible && isMarketplace(surface)) headers.set(edgeHeaders.cache, "1");
  }
  // Manual redirects keep the origin secret and viewer credentials on the configured origin.
  return fetch(
    new Request(target, {
      method: request.method,
      body: request.body,
      headers,
      redirect: "manual",
      cache: "no-store",
    }),
  );
}

async function match(key: string): Promise<Response | undefined> {
  try {
    return await caches.default.match(key);
  } catch {
    return;
  }
}

function deliver(response: Response): Response {
  if (!internalHeaders.some((name) => response.headers.has(name)))
    return response;
  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers: publicHeaders(response.headers),
  });
}

function store(
  key: string,
  response: Response,
  window: Freshness | undefined,
  context: ExecutionContext,
) {
  const seconds = window
    ? Math.ceil((window.expiresAt - Date.now()) / 1000)
    : 0;
  if (!window || seconds <= 0) return;
  const headers = publicHeaders(response.headers);
  headers.set(internalPolicy, headers.get("Cache-Control")!);
  headers.set(edgeHeaders.readAt, String(window.readAt));
  headers.set(internalExpiry, String(window.expiresAt));
  headers.set("Cache-Control", `public, max-age=${seconds}`);
  headers.delete("Age");
  const cached = new Response(response.clone().body, { headers });
  context.waitUntil(caches.default.put(key, cached).catch(() => {}));
}

async function serve(
  request: Request,
  env: Env,
  context: ExecutionContext,
): Promise<Response> {
  const surface = publicReadSurface(request);
  const enabled = isEnabled(env.PUBLIC_READ_EDGE_ENABLED);
  const market = surface !== undefined && isMarketplace(surface);
  if (enabled && market && !isEnabled(env.MARKETPLACE_ENABLED)) {
    return publicResponse(request, env, surface, 404, {
      error: "marketplace_unavailable",
    });
  }
  const origin = configuredOrigin(env.PUBLIC_READ_ORIGIN);
  if (!origin || origin.origin === new URL(request.url).origin) {
    return publicResponse(request, env, surface, 503, {
      error: "sharing_unavailable",
    });
  }

  const mode = publicReadMode(env.PUBLIC_READ_MODE);
  const secret = env.PUBLIC_READ_ORIGIN_SECRET;
  const marketTTL = marketplaceTTL(env.MARKETPLACE_CACHE_TTL_SECONDS);
  const key =
    enabled && surface
      ? await cacheKey(request, origin, surface, mode)
      : undefined;
  const startedAt = Date.now();
  const browserOrigin = request.headers.get("Origin");
  let canQuery = false;
  if (market && key) {
    if (!allowedOrigin(browserOrigin, env)) {
      return publicResponse(request, env, surface, 403, {
        error: "origin not allowed",
      });
    }
    try {
      if (env.MARKETPLACE_RATE_LIMIT) {
        const { success } = await env.MARKETPLACE_RATE_LIMIT.limit({
          key: request.headers.get("CF-Connecting-IP") ?? "unknown",
        });
        if (!success) {
          const response = publicResponse(request, env, surface, 429, {
            error: "rate_limited",
          });
          response.headers.set("Retry-After", "60");
          return response;
        }
        canQuery = true;
      }
    } catch {
      // Keep the existing Go limiter on misses when the edge limiter is unavailable.
    }
  }

  const cacheable = key && (!market || marketTTL > 0);
  if (cacheable) {
    const entry = await match(key);
    if (entry) {
      const lastRead = Number(entry.headers.get(edgeHeaders.readAt));
      const expiresAt = Number(entry.headers.get(internalExpiry));
      if (
        Date.now() < expiresAt &&
        (!market || Date.now() < lastRead + marketTTL * 1000)
      ) {
        if (market) {
          return new Response(entry.body, {
            headers: responseHeaders(browserOrigin, surface),
          });
        }
        const headers = publicHeaders(entry.headers);
        headers.set("Cache-Control", entry.headers.get(internalPolicy)!);
        headers.set(
          "Age",
          String(Math.max(0, Math.floor((startedAt - lastRead) / 1000))),
        );
        return new Response(entry.body, { headers });
      }
    }
  }

  if (
    market &&
    key &&
    canQuery &&
    mode === "hyperdrive" &&
    env.MARKETPLACE_DB
  ) {
    try {
      const result = await readMarketplace(
        env.MARKETPLACE_DB,
        request,
        surface,
        env,
        context,
      );
      if (result) {
        const response = publicResponse(
          request,
          env,
          surface,
          result.status,
          result.body,
        );
        if (cacheable && response.status === 200) {
          store(
            key,
            response,
            { readAt: startedAt, expiresAt: startedAt + marketTTL * 1000 },
            context,
          );
        }
        return response;
      }
    } catch {
      // The origin preserves the read contract on connection and query failures.
    }
  }

  let response: Response;
  try {
    response = await readOrigin(request, origin, surface, secret, !!cacheable);
  } catch {
    return publicResponse(request, env, surface, 503, {
      error: "sharing_unavailable",
    });
  }
  if (cacheable && surface) {
    store(
      key,
      response,
      freshness(response, surface, startedAt, marketTTL, !!secret),
      context,
    );
  }
  return deliver(response);
}

export default { fetch: serve } satisfies ExportedHandler<Env>;
