import { cloudflareTest } from "@cloudflare/vitest-plugin";
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: { fileParallelism: false, testTimeout: 15_000 },
  plugins: [
    cloudflareTest({
      wrangler: { configPath: "./wrangler.jsonc" },
      miniflare: {
        ratelimits: {
          MARKETPLACE_RATE_LIMIT: {
            namespace_id: "1001",
            simple: { limit: 8, period: 60 },
          },
        },
        bindings: {
          PUBLIC_READ_MODE: "go_cache",
          MARKETPLACE_PAGE_SIZE: "3",
          API_ALLOWED_ORIGINS:
            "https://one.example.test,https://two.example.test",
          EDGE_FIXTURE: process.env.EDGE_FIXTURE ?? "",
          PUBLIC_READ_ORIGIN: process.env.EDGE_FIXTURE_ORIGIN ?? "",
          PUBLIC_READ_ORIGIN_SECRET: process.env.EDGE_FIXTURE_SECRET ?? "",
          PUBLIC_READ_EDGE_ENABLED: "true",
          MARKETPLACE_ENABLED: "true",
          MARKETPLACE_CACHE_TTL_SECONDS: "2",
        },
      },
    }),
  ],
});
