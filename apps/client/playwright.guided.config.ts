import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  testMatch: ["design-e13-03.spec.ts", "design-e13-04.spec.ts"],
  use: { baseURL: "http://127.0.0.1:4183" },
  webServer: {
    command: "pnpm exec vite --host 127.0.0.1 --port 4183",
    port: 4183,
    reuseExistingServer: false,
  },
});
