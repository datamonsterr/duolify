import posthog from "posthog-js/dist/module.no-external";
import {
  navigation,
  screenIds,
  type Destination,
  type ScreenId,
} from "../features/shell/navigation";
const key = import.meta.env.VITE_POSTHOG_KEY;
type Identity = {
  learnerId: string;
  optedOut: boolean;
  platform: "web" | "desktop" | "ios" | "android";
  appVersion?: string;
};
let identity: Identity | undefined;
const exposed = new Set<string>();
const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

// Auth supplies the internal Learner UUID; public browsing has no identity.
export function configureAnalytics(value: Identity | null) {
  if (identity?.learnerId !== value?.learnerId) exposed.clear();
  identity = value ?? undefined;
  if (key && value && !value.optedOut && uuid.test(value.learnerId)) {
    try {
      posthog.identify(value.learnerId);
    } catch {
      /* Identity setup must not block the app. */
    }
  }
}
export function exposePricing(
  version: string,
  surface: "paywall" | "quote" | "pack_list" | "plan_list",
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
  } catch {
    return;
  }
  const id = `${identity.learnerId}:${surface}:${version}`;
  try {
    if (exposed.has(id) || sessionStorage.getItem(`pricing-exposure:${id}`))
      return;
  } catch {
    if (exposed.has(id)) return;
  }
  try {
    posthog.capture("pricing_version_exposed", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? "0.1.0",
      source: "client",
      pricing_version: version,
      surface,
    });
    exposed.add(id);
    try {
      sessionStorage.setItem(`pricing-exposure:${id}`, "1");
    } catch {
      /* Memory dedupe still works when device storage is unavailable. */
    }
  } catch {
    /* Analytics must never interrupt a price view. */
  }
}
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

// Compatibility with the theme foundation; auth has one shared analytics boundary.
export type UiAnalyticsContext = {
  learnerId: string;
  platform: "desktop" | "ios" | "android" | "web";
  appVersion: string;
  enabled: boolean;
};
export function configureUiAnalytics(context: UiAnalyticsContext | null) {
  configureAnalytics(
    context
      ? {
          learnerId: context.learnerId,
          platform: context.platform,
          appVersion: context.appVersion,
          optedOut: !context.enabled,
        }
      : null,
  );
}
type CourseControl =
  "open_course" | "refresh_courses" | "retry_courses" | "clear_search";
export function trackControl(controlId: CourseControl, courseId?: string) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("ui_control_activated", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? "0.1.0",
      source: "client",
      surface: "courses",
      control_id: controlId,
      ...(courseId ? { course_id: courseId } : {}),
    });
  } catch {
    // Analytics is derived data; transport failure must never block the control.
  }
}

function trackShell(
  event: "ui_screen_viewed" | "ui_navigation_selected",
  properties: { screen_id: ScreenId } | { destination: Destination },
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture(event, {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? "0.1.0",
      source: "client",
      ...properties,
    });
  } catch {
    /* Analytics never blocks navigation. */
  }
}
export function trackNavigation(destination: Destination) {
  if (
    destination !== "more" &&
    destination !== "streak" &&
    !navigation.some((item) => item.id === destination)
  )
    return;
  trackShell("ui_navigation_selected", { destination });
}
export function trackScreen(screenId: ScreenId) {
  if (!screenIds.includes(screenId)) return;
  trackShell("ui_screen_viewed", { screen_id: screenId });
}
