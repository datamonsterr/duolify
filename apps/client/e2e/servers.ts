// Shared by the browser server configuration and its real-API fixtures.
import type { APIRequestContext } from "@playwright/test";

let pathHash = 0;
for (const character of process.cwd()) {
  pathHash = (pathHash * 31 + character.charCodeAt(0)) >>> 0;
}
const portOffset = pathHash % 1000;
export const webPort = Number(
  process.env.E2E_WEB_PORT ??
    process.env.DUOLIFY_E2E_WEB_PORT ??
    4184 + portOffset,
);
export const webBase = `http://127.0.0.1:${webPort}`;
export const apiBase =
  process.env.E2E_API_BASE ??
  process.env.DUOLIFY_E2E_API_URL ??
  `http://127.0.0.1:${18084 + portOffset}`;

export async function resetAuthRateLimits(request: APIRequestContext) {
  const response = await request.post(`${apiBase}/test/reset-auth-rate-limits`);
  if (!response.ok()) {
    throw new Error(`Could not reset E2E auth throttles: ${response.status()}`);
  }
}
