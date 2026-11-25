import { env as bindings, exports } from "cloudflare:workers";

interface Fixture {
  course: string;
  privateCourse: string;
  moderated: string;
  hyperdriveModerated: string;
  share: string;
  revocable: string;
}

declare global {
  namespace Cloudflare {
    interface GlobalProps {
      mainModule: typeof import("../workers/public-read/index");
    }
  }
}

export const env = bindings as unknown as {
  EDGE_FIXTURE: string;
  MARKETPLACE_DB: Hyperdrive;
  MARKETPLACE_RATE_LIMIT: RateLimit;
  PUBLIC_READ_ORIGIN: string;
  PUBLIC_READ_ORIGIN_SECRET: string;
};

export const fixture = JSON.parse(env.EDGE_FIXTURE) as Fixture;
export const market = "/v1/marketplace/courses";
export const demo = "/v1/courses/shared-demo";
export const revocable = `/v1/public/shares/${fixture.revocable}`;
export const share = `/v1/public/shares/${fixture.share}`;

let visitor = 0;

function requestWithVisitor(path: string, init?: RequestInit) {
  const headers = new Headers(init?.headers);
  if (!headers.has("CF-Connecting-IP")) {
    headers.set("CF-Connecting-IP", `2001:db8::${(++visitor).toString(16)}`);
  }
  return new Request(`https://api.example.test${path}`, { ...init, headers });
}

export function worker(path: string, init?: RequestInit) {
  return exports.default.fetch(requestWithVisitor(path, init));
}

export function origin(path: string, init?: RequestInit) {
  return fetch(`${env.PUBLIC_READ_ORIGIN}${path}`, init);
}

export function action(name: string) {
  return origin(`/__test/${name}`, { method: "POST" });
}

export async function workerInMode(
  mode: "hyperdrive" | "go_cache",
  path: string,
  init?: RequestInit,
  overrides: Record<string, unknown> = {},
) {
  const { createExecutionContext, waitOnExecutionContext } =
    await import("cloudflare:test");
  const { default: handler } = await import("../workers/public-read/index");
  const context = createExecutionContext();
  try {
    return await handler.fetch(
      requestWithVisitor(path, init),
      {
        ...env,
        PUBLIC_READ_EDGE_ENABLED: "true",
        MARKETPLACE_ENABLED: "true",
        MARKETPLACE_CACHE_TTL_SECONDS: "2",
        PUBLIC_READ_MODE: mode,
        ...overrides,
      },
      context,
    );
  } finally {
    await waitOnExecutionContext(context);
  }
}
