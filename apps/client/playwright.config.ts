import { defineConfig } from "@playwright/test";
import { apiBase, webBase, webPort } from "./e2e/servers";
export default defineConfig({
  testDir: "./e2e",
  use: { baseURL: webBase },
  webServer: [
    {
      command: `pnpm preview --host 127.0.0.1 --port ${webPort}`,
      port: webPort,
      reuseExistingServer: false,
    },
    {
      command: `E2E_API_ADDR=${new URL(apiBase).host} mise run api:pricing-e2e-server`,
      url: `${apiBase}/healthz`,
      timeout: 120_000,
      reuseExistingServer: false,
    },
  ],
});
