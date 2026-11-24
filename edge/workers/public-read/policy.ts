export type Surface =
  "marketplace" | "course_page" | "share_preview" | "shared_demo";
export type MarketplaceSurface = "marketplace" | "course_page";

export const edgeHeaders = {
  secret: "X-Duolify-Edge-Secret",
  clientIP: "X-Duolify-Edge-Client-IP",
  cache: "X-Duolify-Edge-Cache",
  legacyApproval: "X-Duolify-Edge-Cache-Valid-Until",
  readAt: "X-Duolify-Edge-Read-At",
  ttl: "X-Duolify-Edge-TTL",
} as const;

export interface Freshness {
  readAt: number;
  expiresAt: number;
}

const marketplace = "/v1/marketplace/courses";
const marketParams = new Set([
  "q",
  "category",
  "language",
  "sort",
  "cursor",
  "limit",
]);

export type PublicReadMode = "hyperdrive" | "go_cache";

export function publicReadMode(value: string | undefined): PublicReadMode {
  return !value || value === "hyperdrive" ? "hyperdrive" : "go_cache";
}

export function isEnabled(value: string | undefined): boolean {
  return value === "true";
}

function wholeNumber(value: string | null | undefined): number {
  if (!value || !/^\d+$/.test(value)) return 0;
  const number = Number(value);
  return Number.isSafeInteger(number) ? number : 0;
}

export function marketplaceTTL(value: string | null | undefined): number {
  const seconds = wholeNumber(value);
  return seconds <= 3600 ? seconds : 0;
}

export function isMarketplace(surface: Surface): surface is MarketplaceSurface {
  return surface === "marketplace" || surface === "course_page";
}

export function publicReadSurface(request: Request): Surface | undefined {
  if (request.method !== "GET") return;
  const { pathname } = new URL(request.url);
  if (pathname === "/v1/courses/shared-demo") return "shared_demo";
  if (/^\/v1\/public\/shares\/[^/]+$/.test(pathname)) return "share_preview";
  if (pathname === marketplace) return "marketplace";
  if (/^\/v1\/marketplace\/courses\/[^/]+$/.test(pathname))
    return "course_page";
}

export async function cacheKey(
  request: Request,
  origin: URL,
  surface: Surface | undefined,
  mode: PublicReadMode = "hyperdrive",
): Promise<string | undefined> {
  if (!surface || surface === "share_preview") return;
  const headers = request.headers;
  for (const name of headers.keys()) {
    if (
      name.startsWith("if-") ||
      [
        "authorization",
        "proxy-authorization",
        "cookie",
        "range",
        "cache-control",
        "pragma",
      ].includes(name)
    )
      return;
  }
  // Keep the authored demo on the Go path for cross-origin browser requests.
  if (surface === "shared_demo" && headers.has("origin")) return;

  const url = new URL(request.url);
  if (url.search.length > 4096 || url.search.includes(";")) return;
  try {
    // Go and URLSearchParams handle malformed percent escapes differently.
    decodeURIComponent(url.search);
  } catch {
    return;
  }
  const seen = new Set<string>();
  const query = url.searchParams;
  for (const name of query.keys()) {
    if (surface !== "marketplace" || !marketParams.has(name) || seen.has(name))
      return;
    seen.add(name);
  }
  query.sort();

  const key = JSON.stringify([
    origin.origin,
    isMarketplace(surface) ? mode : "demo",
    url.pathname,
    query.toString(),
    headers.get("accept-language"),
  ]);
  const bytes = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(key),
  );
  const digest = Array.from(new Uint8Array(bytes), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  return new URL(`/.duolify/public-read/v2/${digest}`, url.origin).href;
}

export function freshness(
  response: Response,
  surface: Surface,
  requestedAt: number,
  ttl: number,
  trusted: boolean,
): Freshness | undefined {
  const h = response.headers;
  const varies =
    h
      .get("vary")
      ?.toLowerCase()
      .split(",")
      .map((name) => name.trim()) ?? [];
  if (
    response.status !== 200 ||
    h.has("set-cookie") ||
    h.has("content-encoding") ||
    varies.some(
      (name) =>
        !["origin", "accept-language", "accept-encoding"].includes(name),
    ) ||
    h.get("content-type")?.split(";")[0] !== "application/json"
  )
    return;

  const policy =
    h
      .get("cache-control")
      ?.toLowerCase()
      .split(",")
      .map((value) => value.trim()) ?? [];
  const age = wholeNumber(h.get("age"));
  if (isMarketplace(surface)) {
    const sourceAt = wholeNumber(h.get(edgeHeaders.readAt));
    const originTTL = marketplaceTTL(h.get(edgeHeaders.ttl));
    if (
      !trusted ||
      !sourceAt ||
      !originTTL ||
      !ttl ||
      !policy.includes("no-store")
    )
      return;
    const readAt = Math.min(requestedAt - age * 1000, sourceAt);
    return { readAt, expiresAt: readAt + Math.min(originTTL, ttl) * 1000 };
  }
  if (
    surface !== "shared_demo" ||
    !policy.includes("public") ||
    policy.includes("private") ||
    policy.includes("no-store") ||
    policy.includes("no-cache") ||
    !/^"shared-demo-[a-z0-9-]+"$/.test(h.get("etag") ?? "")
  )
    return;
  const maxAge = wholeNumber(
    policy.find((value) => value.startsWith("max-age="))?.slice(8),
  );
  if (!maxAge) return;
  const date = Date.parse(h.get("date") ?? "");
  const readAt = Math.min(
    requestedAt - age * 1000,
    Number.isFinite(date) ? date : requestedAt,
  );
  return { readAt, expiresAt: readAt + maxAge * 1000 };
}
