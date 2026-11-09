import posthog from "posthog-js/dist/module.no-external";
const key = import.meta.env.VITE_POSTHOG_KEY;
if (key)
  posthog.init(key, {
    api_host: import.meta.env.VITE_POSTHOG_HOST || "https://us.i.posthog.com",
    defaults: "2026-08-30",
    autocapture: false,
    capture_pageview: false,
  });
export function track(
  event: "lesson_started" | "slide_viewed" | "lesson_completed",
  properties: Record<string, string | number>,
) {
  if (key) posthog.capture(event, properties);
}
