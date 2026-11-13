import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: "lesson-offline-shell",
      generateBundle(_options, bundle) {
        const files = Array.from(
          new Set([
            "/index.html",
            ...Object.keys(bundle)
              .filter((name) => name.startsWith("assets/"))
              .map((name) => `/${name}`),
          ]),
        );
        this.emitFile({
          type: "asset",
          fileName: "lesson-shell-assets.json",
          source: JSON.stringify(files),
        });
      },
    },
  ],
  server: { port: 1420, strictPort: true },
  build: {
    rollupOptions: {
      input: { app: "index.html", guidedChat: "guided-chat.html" },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    exclude: ["e2e/**", "node_modules/**"],
  },
});
