import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e",
  use: { baseURL: "http://127.0.0.1:4182" },
  webServer: [
    {
      command: "pnpm preview --host 127.0.0.1 --port 4182",
      port: 4182,
      reuseExistingServer: false,
    },
    {
      command: "mise run api:pricing-e2e-server",
      url: "http://127.0.0.1:18081/healthz",
      timeout: 120_000,
      reuseExistingServer: false,
    },
  ],
});
