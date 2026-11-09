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

// E1-06 installs authenticated, consent-aware context here. Missing context emits nothing.
export type UiAnalyticsContext = {
  learnerId: string;
  platform: "desktop" | "ios" | "android" | "web";
  appVersion: string;
  enabled: boolean;
};
let uiContext: UiAnalyticsContext | null = null;
export function configureUiAnalytics(context: UiAnalyticsContext | null) {
  uiContext = context;
}
type CourseControl =
  "open_course" | "refresh_courses" | "retry_courses" | "clear_search";
export function trackControl(controlId: CourseControl, courseId?: string) {
  if (
    !uiContext?.enabled ||
    !/^[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}$/i.test(
      uiContext.learnerId,
    )
  )
    return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("ui_control_activated", {
      distinct_id: uiContext.learnerId,
      platform: uiContext.platform,
      app_version: uiContext.appVersion,
      source: "client",
      surface: "courses",
      control_id: controlId,
      ...(courseId ? { course_id: courseId } : {}),
    });
  } catch {
    // Analytics is derived data; transport failure must never block the control.
  }
}
