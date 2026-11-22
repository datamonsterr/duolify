import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import {
  buildOfflineManifest,
  type BundleLike,
} from "./build/offlineManifest.ts";
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: "lesson-offline-shell",
      generateBundle(_options, bundle) {
        this.emitFile({
          type: "asset",
          fileName: "lesson-shell-assets.json",
          source: JSON.stringify(
            buildOfflineManifest(bundle as unknown as BundleLike),
          ),
        });
      },
    },
  ],
  // A new service-worker URL per build makes the browser install it and rebuild the shell cache.
  define: { __BUILD_ID__: JSON.stringify(Date.now().toString(36)) },
  server: { port: 1420, strictPort: true },
  // Browser-session e2e (playwright.session.config.ts): serve the same-origin build with the API
  // proxied under /v1, as Caddy does in production.
  preview: process.env.E2E_PROXY_API
    ? { proxy: { "/v1": { target: process.env.E2E_PROXY_API } } }
    : {},
  build: {
    rollupOptions: {
      input: { app: "index.html", guidedChat: "guided-chat.html" },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    exclude: ["e2e/**", "e2e-session/**", "node_modules/**"],
    // CI: GitHub annotations plus JUnit for the job summary (scripts/ci/junit-summary.py).
    reporters: process.env.CI
      ? ["default", "github-actions", "junit"]
      : ["default"],
    outputFile: {
      junit: process.env.VITEST_JUNIT_FILE ?? "./test-results/vitest-junit.xml",
    },
  },
});
