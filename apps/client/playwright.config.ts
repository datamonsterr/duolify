import { defineConfig } from "@playwright/test";
import { apiBase, authRateLimits, webBase, webPort } from "./e2e/servers";
export default defineConfig({
  testDir: "./e2e",
  workers: Number(process.env.E2E_WORKERS ?? 1),
  use: { baseURL: webBase },
  webServer: [
    {
      command: `pnpm preview --host 127.0.0.1 --port ${webPort}`,
      port: webPort,
      reuseExistingServer: false,
    },
    {
      command: `DUOLIFY_ENV=development FEATURE_CUSTOM_CHARACTERS=true AUTH_CODE_REQUESTS_PER_IP_PER_HOUR=${authRateLimits.requestIp} AUTH_CODE_REQUESTS_PER_EMAIL_PER_HOUR=${authRateLimits.requestEmail} AUTH_CODE_VERIFICATIONS_PER_IP_PER_HOUR=${authRateLimits.verifyIp} E2E_API_ADDR=${new URL(apiBase).host} E2E_WEB_PORT=${webPort} LESSON_TEST_TRACER_ENABLED=true mise run api:pricing-e2e-server`,
      url: `${apiBase}/healthz`,
      timeout: 120_000,
      reuseExistingServer: false,
    },
  ],
});
