// Shared by the browser server configuration and its real-API fixtures.
import type { APIRequestContext } from "@playwright/test";

export const webPort = Number(
  process.env.E2E_WEB_PORT ?? process.env.DUOLIFY_E2E_WEB_PORT ?? 4020,
);
export const webBase = `http://127.0.0.1:${webPort}`;
export const apiBase =
  process.env.E2E_API_BASE ??
  process.env.DUOLIFY_E2E_API_URL ??
  "http://127.0.0.1:4021";

export const authRateLimits = {
  requestIp: 100000,
  requestEmail: 100000,
  verifyIp: 100000,
};

export async function resetAuthRateLimits(request: APIRequestContext) {
  const response = await request.post(`${apiBase}/test/reset-auth-rate-limits`);
  if (!response.ok()) {
    throw new Error(`Could not reset E2E auth throttles: ${response.status()}`);
  }
}
