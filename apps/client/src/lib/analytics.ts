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
  try {
    if (value?.optedOut) posthog.opt_out_capturing();
    else if (value) posthog.opt_in_capturing();
  } catch {
    /* Consent state is enforced again by the local boundary below. */
  }
  if (key && value && !value.optedOut && uuid.test(value.learnerId)) {
    try {
      posthog.identify(value.learnerId);
    } catch {
      /* Identity setup must not block the app. */
    }
  }
}
// Apply consent before any network request. PostHog's opt-out disables capture
// and persistence and clears its stored event queue.
export function setAnalyticsConsent(enabled: boolean) {
  if (!identity) return;
  identity = { ...identity, optedOut: !enabled };
  try {
    if (enabled) posthog.opt_in_capturing();
    else posthog.opt_out_capturing();
  } catch {
    /* The local boundary still suppresses events if the SDK is unavailable. */
  }
}
export function trackAppearanceChanged(mode: "system" | "light" | "dark") {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("ui_appearance_changed", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? "0.1.0",
      source: "client",
      mode,
    });
  } catch {
    /* Analytics is derived and never blocks settings. */
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
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    if (key)
      posthog.capture(event, {
        ...properties,
        distinct_id: identity.learnerId,
        platform: identity.platform,
        app_version: identity.appVersion ?? "0.1.0",
        source: "client",
      });
  } catch {
    /* Analytics must not interrupt a lesson. */
  }
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
// E1-02 catalogue: stable identifiers, authenticated UUID, no learner text.
export function trackAuthScreen(screenId: "welcome" | "sign_in") {
  captureAuth("ui_screen_viewed", { screen_id: screenId });
}
export function trackSignIn() {
  captureAuth("ui_control_activated", {
    surface: "sign_in",
    control_id: "sign_in",
  });
}
function captureOAuthUI(
  event: string,
  properties: Record<string, string>,
  distinctID: string,
  optedOut: boolean,
) {
  if (optedOut || !uuid.test(distinctID)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture(event, {
      distinct_id: distinctID,
      platform:
        identity?.platform ??
        (typeof window !== "undefined" && "__TAURI_INTERNALS__" in window
          ? "desktop"
          : "web"),
      app_version: identity?.appVersion ?? "0.1.0",
      source: "client",
      ...properties,
    });
  } catch {
    /* Sign-in continues if analytics is unavailable. */
  }
}
export function trackOAuthStarted(distinctID: string, optedOut: boolean) {
  captureOAuthUI(
    "ui_control_activated",
    { surface: "sign_in", control_id: "sign_in" },
    distinctID,
    optedOut,
  );
}
export function trackOAuthCancelled(distinctID: string, optedOut: boolean) {
  captureOAuthUI(
    "ui_recovery_selected",
    { surface: "sign_in", error_code: "auth_cancelled", action: "retry" },
    distinctID,
    optedOut,
  );
}
function captureAuth(event: string, properties: Record<string, string>) {
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
    /* Analytics delivery cannot block sign-in. */
  }
}

// E7-04: a closed projection excludes drafts, notes, URLs and callback data.
export function trackPurchase(value: {
  event:
    | "paywall_viewed"
    | "credits_upsell_shown"
    | "ui_purchase_option_selected"
    | "ui_checkout_requested";
  pricingVersion: string;
  feature?: string;
  balance?: number;
  nextReset?: string;
  offerId?: string;
  billingPeriod?: "one_time" | "monthly" | "yearly";
}) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture(value.event, {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? "0.1.0",
      source: "client",
      price_table_version: value.pricingVersion,
      ...(value.feature ? { feature: value.feature } : {}),
      ...(value.balance !== undefined ? { balance: value.balance } : {}),
      ...(value.nextReset ? { next_daily_grant_at: value.nextReset } : {}),
      ...(value.offerId ? { offer_id: value.offerId } : {}),
      ...(value.billingPeriod ? { billing_period: value.billingPeriod } : {}),
      ...(value.event === "paywall_viewed"
        ? { placement: "credits_recovery", plan_id: "none" }
        : {}),
    });
  } catch {
    /* Analytics must not interrupt recovery. */
  }
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

// E1-07 catalogue: openspec/changes/export-delete-account-data.
export function trackPrivacyControl(
  controlId: "export_data" | "delete_account",
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("ui_control_activated", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? "0.1.0",
      source: "client",
      surface: "settings_privacy",
      control_id: controlId,
    });
  } catch {
    /* A privacy control remains usable when analytics is unavailable. */
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

// E13-03 catalogue: openspec/changes/shared-guided-chat/specs/design-system/spec.md.
// Build properties explicitly so callers cannot smuggle draft values into events.
const guidedSurfaces = [
  "guided_chat",
  "summary",
  "outline",
  "sheet",
  "course_brief",
] as const;
const guidedControls = [
  "outline_change_requested",
  "proposal_apply",
  "proposal_discard",
  "outline_move",
  "outline_rename",
  "outline_remove",
  "outline_add",
  "outline_undo",
  "confirm_brief",
  "quick_reply",
  "send",
  "skip",
  "form_fallback",
  "retry",
  "confirm",
  "edit",
  "move_up",
  "move_down",
  "drag_reorder",
  "rename",
  "remove",
  "add",
  "undo",
  "apply",
  "discard",
  "open",
  "close",
] as const;
const guidedSheets = [
  "course_brief",
  "setup_summary",
  "outline_chat",
  "fixture_sheet",
  "quick_note",
] as const;
export type GuidedSheetId = (typeof guidedSheets)[number];
export type GuidedUiEvent =
  | { event: "ui_sheet_opened"; sheetId: GuidedSheetId }
  | {
      event: "ui_control_activated";
      surface: (typeof guidedSurfaces)[number];
      controlId: (typeof guidedControls)[number];
    };
export function trackGuidedUi(value: GuidedUiEvent) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  const properties: Record<string, string> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? "0.1.0",
    source: "client",
  };
  if (
    value.event === "ui_sheet_opened" &&
    guidedSheets.includes(value.sheetId)
  ) {
    properties.sheet_id = value.sheetId;
  } else if (
    value.event === "ui_control_activated" &&
    guidedSurfaces.includes(value.surface) &&
    guidedControls.includes(value.controlId)
  ) {
    properties.surface = value.surface;
    properties.control_id = value.controlId;
  } else return;
  try {
    if (!posthog.has_opted_out_capturing())
      posthog.capture(value.event, properties);
  } catch {
    // UI works even when analytics is unavailable.
  }
}

// Catalogue: session-rotation-revocation/specs/design-system/spec.md.
export function trackSessionRecovery() {
  captureAuth("ui_recovery_selected", {
    surface: "sign_in",
    error_code: "session_expired",
    action: "sign_in",
  });
}

// E13-04 catalogue: localized-appearance/specs/design-system/spec.md.
function trackSettings(
  event: "ui_appearance_changed" | "ui_control_activated",
  properties:
    | { mode: "system" | "light" | "dark" }
    | { surface: "settings"; control_id: "locale" | "motion" | "reset" },
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (!posthog.has_opted_out_capturing())
      posthog.capture(event, {
        distinct_id: identity.learnerId,
        platform: identity.platform,
        app_version: identity.appVersion ?? "0.1.0",
        source: "client",
        ...properties,
      });
  } catch {
    /* Preference changes never depend on telemetry. */
  }
}
export function trackAppearance(mode: "system" | "light" | "dark") {
  if (!["system", "light", "dark"].includes(mode)) return;
  trackSettings("ui_appearance_changed", { mode });
}
export function trackPreference(control: "locale" | "motion" | "reset") {
  if (!["locale", "motion", "reset"].includes(control)) return;
  trackSettings("ui_control_activated", {
    surface: "settings",
    control_id: control,
  });
}
export function trackCourseRecovery(
  surface: "course_create" | "courses",
  action: "retry" | "resume",
  errorCode?: string,
  courseId?: string,
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("ui_recovery_selected", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? "0.1.0",
      source: "client",
      surface,
      action,
      ...(errorCode ? { error_code: errorCode } : {}),
      ...(courseId ? { course_id: courseId } : {}),
    });
  } catch {
    /* Telemetry failure never blocks recovery. */
  }
}

export function trackQuickNoteCreated(value: {
  source: "slide" | "lesson" | "chat";
  length: number;
}) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("quick_note_created", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? "0.1.0",
      source: value.source,
      length: value.length,
    });
  } catch {
    /* Analytics failure never blocks note creation. */
  }
}
