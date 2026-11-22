// Web session persistence (openspec/changes/persist-web-session). The cookie session needs the
// API on the page's own origin, as Caddy serves it in production, so this suite serves a build
// made with VITE_API_URL="" and proxies /v1 to the disposable e2e API. `mise run client:e2e-session`.
import { defineConfig, devices } from "@playwright/test";
import { sessionApi, sessionWeb, webPort, apiPort } from "./e2e-session/ports";

export default defineConfig({
  testDir: "./e2e-session",
  workers: 1,
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  use: { ...devices["Desktop Chrome"], baseURL: sessionWeb },
  webServer: [
    {
      command: `E2E_PROXY_API=${sessionApi} pnpm exec vite preview --host 127.0.0.1 --port ${webPort} --strictPort --outDir dist-session`,
      port: webPort,
      reuseExistingServer: false,
    },
    {
      command: `DUOLIFY_ENV=development ALLOW_UNAPPROVED_PRICING=true API_ALLOWED_ORIGINS=${sessionWeb} E2E_API_ADDR=127.0.0.1:${apiPort} E2E_WEB_PORT=${webPort} mise run api:pricing-e2e-server`,
      url: `${sessionApi}/healthz`,
      timeout: 120_000,
      reuseExistingServer: false,
    },
  ],
});
