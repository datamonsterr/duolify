// Full-stack flow suite (openspec/changes/fullstack-flow-e2e): drives the real web
// client on a running stack. No webServer and no page.route mocking of our own API.
// Local: `mise run client:e2e-stack-up` then `mise run client:e2e-stack`.
// Remote: E2E_STACK_URL=https://staging.duolify.online mise run client:e2e-stack.
import { defineConfig, devices } from "@playwright/test";
import { live, stackURL } from "./e2e-stack/stack";

export default defineConfig({
  testDir: "./e2e-stack",
  // *.flow.ts so Vitest's default *.spec/*.test glob never picks these up.
  testMatch: /.*\.flow\.ts$/,
  outputDir: "./test-results/stack",
  workers: 1,
  retries: 0,
  // Live provider calls (E2E_LIVE=1) are slower than the deterministic mock engine.
  timeout: live ? 600_000 : 120_000,
  expect: { timeout: live ? 120_000 : 15_000 },
  reporter: [
    ...(process.env.CI
      ? ([
          ["github"],
          ["html", { open: "never", outputFolder: "playwright-report-stack" }],
          ["junit", { outputFile: "test-results/stack-junit.xml" }],
        ] as const)
      : []),
    ["list"],
    ["./e2e-stack/flow-reporter.ts"],
  ],
  use: {
    ...devices["Desktop Chrome"],
    baseURL: stackURL,
    viewport: { width: 1280, height: 900 },
    trace: "retain-on-failure",
  },
});
