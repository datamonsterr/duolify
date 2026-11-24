import { cloudflareTest } from "@cloudflare/vitest-plugin";
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["test/**/chat*.test.ts"],
    setupFiles: ["./test/chat-setup.ts"],
    fileParallelism: false,
    maxWorkers: 1,
    isolate: false,
    testTimeout: 15_000,
  },
  plugins: [
    cloudflareTest({
      wrangler: { configPath: "./workers/chat/wrangler.jsonc" },
      miniflare: {
        bindings: {
          CHAT_EDGE_ENABLED: "true",
          CHAT_API_ORIGIN: "https://go.example.test",
          API_ALLOWED_ORIGINS: "https://client.example.test",
          EDGE_SERVICE_SECRET: "edge-test-shared-secret-only-123456",
        },
      },
    }),
  ],
});
