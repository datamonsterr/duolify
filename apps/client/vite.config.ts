import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
export default defineConfig({
  ...(process.env.E2E_API_BASE
    ? {
        define: {
          "import.meta.env.VITE_API_URL": JSON.stringify(
            `http://127.0.0.1:${process.env.E2E_WEB_PORT}`,
          ),
        },
        preview: {
          proxy: {
            "/v1": {
              target: process.env.E2E_API_BASE,
              changeOrigin: true,
              configure(proxy) {
                proxy.on("proxyReq", (request) =>
                  request.removeHeader("origin"),
                );
              },
            },
          },
        },
      }
    : {}),
  plugins: [react(), tailwindcss()],
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
