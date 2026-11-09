import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e",
  use: { baseURL: "http://127.0.0.1:4184" },
  webServer: [
    {
      command: "pnpm preview --host 127.0.0.1 --port 4184",
      port: 4184,
      reuseExistingServer: false,
    },
    {
      command: "E2E_API_ADDR=127.0.0.1:18084 mise run api:pricing-e2e-server",
      url: "http://127.0.0.1:18084/healthz",
      timeout: 120_000,
      reuseExistingServer: false,
    },
  ],
});
