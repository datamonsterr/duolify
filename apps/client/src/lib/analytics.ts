import posthog from "posthog-js/dist/module.no-external";
const key = import.meta.env.VITE_POSTHOG_KEY;
type Identity = {
  learnerId: string;
  optedOut: boolean;
  platform: "web" | "desktop" | "ios" | "android";
};
let identity: Identity | undefined;
const exposed = new Set<string>();
const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

// Auth supplies the internal Learner UUID; public browsing has no identity.
export function configureAnalytics(value: Identity) {
  if (identity?.learnerId !== value.learnerId) exposed.clear();
  identity = value;
  if (key && !value.optedOut && uuid.test(value.learnerId)) {
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
  if (posthog.has_opted_out_capturing()) return;
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
      app_version: "0.1.0",
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
