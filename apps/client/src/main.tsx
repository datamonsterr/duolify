import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { createQueryClient } from "./lib/queryClient";
import { registerCourseQueryClient } from "./lib/courseQueries";
import App from "./App";
import { session } from "./features/accounts/session";
import { loadArabic } from "./features/design/localization/messages";
import "./styles.css";
const queryClient = createQueryClient();
registerCourseQueryClient(queryClient);
async function start() {
  if (
    import.meta.env.PROD &&
    import.meta.env.VITE_MOCK_API !== "true" &&
    "serviceWorker" in navigator
  ) {
    void navigator.serviceWorker
      .register(`/lesson-shell-sw.js?v=${__BUILD_ID__}`)
      .catch(() => {});
  }
  if (import.meta.env.DEV || import.meta.env.VITE_MOCK_API === "true") {
    const { worker } = await import("./mocks/browser");
    await worker.start({ onUnhandledFrame: "bypass" });
  }
  // Avoid a flash of English for an Arabic session: fetch the chunk before first paint.
  if (session.current()?.locale === "ar") await loadArabic().catch(() => {});
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>
    </React.StrictMode>,
  );
}
void start();
