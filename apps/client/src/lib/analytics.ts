import { appVersion, clientPlatform } from "./client-build";
import { posthog } from "./posthog";
import type { ShortcutId } from "../features/hotkeys/registry";
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
const shownQuotes = new Set<string>();
const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

// Auth supplies the internal Learner UUID; public browsing has no identity.
export function configureAnalytics(value: Identity | null) {
  if (identity?.learnerId !== value?.learnerId) {
    exposed.clear();
    shownQuotes.clear();
  }
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

const quoteFeatures = new Set([
  "preview",
  "outline",
  "lesson_content",
  "narration",
  "tutor_short",
  "tutor_long",
  "quick_note",
  "cached_replay",
  "setup_chat",
  "course_brief_chat",
  "outline_chat_change",
  "outline_replan",
  "character_create",
]);
export function trackQuoteShown(value: {
  quote_id: string;
  feature: string;
  pricing_version: string;
}) {
  if (
    !identity ||
    identity.optedOut ||
    !uuid.test(identity.learnerId) ||
    !uuid.test(value.quote_id) ||
    !quoteFeatures.has(value.feature) ||
    !/^[A-Za-z0-9_.:-]{1,100}$/.test(value.pricing_version) ||
    shownQuotes.has(value.quote_id)
  )
    return;
  const storageKey = `quote-shown:${identity.learnerId}:${value.quote_id}`;
  try {
    if (sessionStorage.getItem(storageKey)) return;
  } catch {
    /* In-memory deduplication still applies without storage. */
  }
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("action_quote_shown", {
      quote_id: value.quote_id,
      feature: value.feature,
      pricing_version: value.pricing_version,
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
    });
    shownQuotes.add(value.quote_id);
    try {
      sessionStorage.setItem(storageKey, "1");
    } catch {
      /* In-memory deduplication still applies without storage. */
    }
  } catch {
    /* Analytics must not interrupt a quote. */
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
      app_version: identity.appVersion ?? appVersion,
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
      app_version: identity.appVersion ?? appVersion,
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
export function track(
  event:
    | "lesson_started"
    | "slide_viewed"
    | "lesson_generation_requested"
    | "lesson_generated"
    | "course_generation_failed"
    | "lesson_test_started"
    | "lesson_test_submitted"
    | "lesson_test_passed"
    | "lesson_test_answer_wrong"
    | "retry_variant_queued"
    | "retry_variant_served"
    | "retry_variant_answered"
    | "lesson_test_recap_shown"
    | "ui_test_retry_presented",
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
        app_version: identity.appVersion ?? appVersion,
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
function captureSSOUI(
  event: string,
  properties: Record<string, string>,
  distinctID: string,
  optedOut: boolean,
) {
  if (optedOut || !uuid.test(distinctID)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture(event, {
      ...properties,
      distinct_id: distinctID,
      platform:
        identity?.platform ??
        clientPlatform(
          typeof window !== "undefined" && "__TAURI_INTERNALS__" in window,
          import.meta.env.VITE_MOBILE_PLATFORM,
        ),
      app_version: identity?.appVersion ?? appVersion,
      source: "client",
    });
  } catch {
    /* Sign-in continues if analytics is unavailable. */
  }
}
export function trackSSOStarted(distinctID: string, optedOut: boolean) {
  captureSSOUI(
    "ui_control_activated",
    { surface: "sign_in", control_id: "sign_in" },
    distinctID,
    optedOut,
  );
}
export function trackSSOCancelled(distinctID: string, optedOut: boolean) {
  captureSSOUI(
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
      ...properties,
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
    });
  } catch {
    /* Analytics delivery cannot block sign-in. */
  }
}

export function trackHotkey(
  keyId: ShortcutId,
  surface: ScreenId,
  entryPoint: "hotkey" | "button" = "hotkey",
) {
  if (!screenIds.includes(surface)) return;
  captureAuth("hotkey_used", {
    key_id: keyId,
    surface,
    entry_point: entryPoint,
  });
}

// Fixed authored identifiers only; catalogue precedes this emitter (E2-05).
export function trackSharedDemo(
  surface: "welcome" | "course_brief",
  viewed = false,
) {
  if (surface !== "welcome" && surface !== "course_brief") return;
  if (!viewed)
    captureAuth("ui_control_activated", { surface, control_id: "shared_demo" });
  else
    captureAuth("course_preview_viewed", {
      preview_kind: "shared_demo",
      demo_version: "sky-v1",
    });
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
      app_version: identity.appVersion ?? appVersion,
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
  | "open_course"
  | "refresh_courses"
  | "retry_courses"
  | "clear_search"
  | "download_library";
export function trackControl(controlId: CourseControl, courseId?: string) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("ui_control_activated", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      surface: "courses",
      control_id: controlId,
      ...(courseId ? { course_id: courseId } : {}),
    });
  } catch {
    // Analytics is derived data; transport failure must never block the control.
  }
}

// Example courses catalogue: openspec/changes/example-courses. Closed enum only, no course text.
export function trackExampleCoursesViewed(
  surface: "courses" | "learn_empty" | "onboarding",
) {
  if (!["courses", "learn_empty", "onboarding"].includes(surface)) return;
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("example_courses_viewed", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      surface,
    });
  } catch {
    // Analytics is derived data; transport failure must never block the screen.
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
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      surface: "settings_privacy",
      control_id: controlId,
    });
  } catch {
    /* A privacy control remains usable when analytics is unavailable. */
  }
}

// E5-06 catalogue: learning analytics and history export controls.
export function trackLearningAnalyticsControl(
  controlId: "export_history" | "export_analytics",
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("ui_control_activated", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      surface: "learning_analytics",
      control_id: controlId,
    });
  } catch {
    /* Analytics failure never blocks export. */
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
      ...properties,
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
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
  "course_steering",
  "course_brief",
  "setup_summary",
  "outline_chat",
  "fixture_sheet",
  "quick_note",
  "lesson_tutor",
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
    app_version: identity.appVersion ?? appVersion,
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

// E13-04 & E13-06 catalogue: design-system/spec.md.
type MascotEvent =
  | {
      event: "mascot_customized";
      mode: "default" | "character";
      fields_changed: string;
    }
  | { event: "mascot_companion_toggled"; visible: boolean }
  | {
      event: "mascot_state_fallback";
      reason: "art_failed" | "character_unavailable";
    };
const MASCOT_FIELDS = ["palette", "accessories", "character"];
/** Content-free mascot events; `fields_changed` is a comma list of field names only. */
export function trackMascot(e: MascotEvent) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  if (
    e.event === "mascot_customized" &&
    !e.fields_changed.split(",").every((f) => MASCOT_FIELDS.includes(f))
  )
    return;
  try {
    const { event, ...properties } = e;
    if (!posthog.has_opted_out_capturing())
      posthog.capture(event, {
        ...properties,
        distinct_id: identity.learnerId,
        platform: identity.platform,
        app_version: identity.appVersion ?? appVersion,
        source: "client",
      });
  } catch {
    /* Preference changes never depend on telemetry. */
  }
}
function trackSettings(
  event: "ui_appearance_changed" | "ui_control_activated",
  properties:
    | { mode: "system" | "light" | "dark" }
    | {
        surface: "settings";
        control_id: "locale" | "motion" | "reset" | "ui_sounds" | "haptics";
      },
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (!posthog.has_opted_out_capturing())
      posthog.capture(event, {
        ...properties,
        distinct_id: identity.learnerId,
        platform: identity.platform,
        app_version: identity.appVersion ?? appVersion,
        source: "client",
      });
  } catch {
    /* Preference changes never depend on telemetry. */
  }
}
export function trackAppearance(mode: "system" | "light" | "dark") {
  if (!["system", "light", "dark"].includes(mode)) return;
  trackSettings("ui_appearance_changed", { mode });
}
export function trackPreference(
  control: "locale" | "motion" | "reset" | "ui_sounds" | "haptics",
) {
  if (!["locale", "motion", "reset", "ui_sounds", "haptics"].includes(control))
    return;
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
      app_version: identity.appVersion ?? appVersion,
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

export function trackTutorRecovery(
  action: "retry" | "reload" | "top_up" | "read_transcript",
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("ui_recovery_selected", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      surface: "lesson_tutor",
      action,
    });
  } catch {
    /* Telemetry failure never blocks recovery. */
  }
}

/** E8-05 catalogue: the Learner confirmed deleting a tutor thread. Identifiers only, never text. */
export function trackTutorControl(controlId: "delete_thread") {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("ui_control_activated", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      surface: "tutor_chat",
      control_id: controlId,
    });
  } catch {
    /* Telemetry failure never blocks deleting. */
  }
}

export function trackQuickNoteReviewed(noteId: string) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("quick_note_reviewed", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      note_id: noteId,
    });
  } catch {
    /* Analytics failure never blocks review. */
  }
}

export function trackQuickNoteDeleted(noteId: string) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("quick_note_deleted", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      note_id: noteId,
    });
  } catch {
    /* Analytics failure never blocks delete. */
  }
}

export function trackExportNotes() {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("ui_control_activated", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      surface: "quick_notes",
      control_id: "export_notes",
    });
  } catch {
    /* Analytics failure never blocks export. */
  }
}

export function trackBookmarkAdded(value: {
  targetType: "slide" | "lesson";
  courseId?: string;
  lessonId?: string;
  slideId?: number | string;
}) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("bookmark_added", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      target_type: value.targetType,
      ...(value.courseId ? { course_id: value.courseId } : {}),
      ...(value.lessonId ? { lesson_id: value.lessonId } : {}),
      ...(value.slideId !== undefined ? { slide_id: value.slideId } : {}),
    });
  } catch {
    /* Analytics failure never blocks bookmarking. */
  }
}

export function trackBookmarkRemoved(value: {
  targetType: "slide" | "lesson";
  courseId?: string;
  lessonId?: string;
  slideId?: number | string;
}) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("bookmark_removed", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      target_type: value.targetType,
      ...(value.courseId ? { course_id: value.courseId } : {}),
      ...(value.lessonId ? { lesson_id: value.lessonId } : {}),
      ...(value.slideId !== undefined ? { slide_id: value.slideId } : {}),
    });
  } catch {
    /* Analytics failure never blocks bookmark removal. */
  }
}

export function trackBookmarkOpened(value: {
  targetType: "slide" | "lesson";
  courseId?: string;
  lessonId?: string;
  slideId?: number | string;
}) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  try {
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture("bookmark_opened", {
      distinct_id: identity.learnerId,
      platform: identity.platform,
      app_version: identity.appVersion ?? appVersion,
      source: "client",
      target_type: value.targetType,
      ...(value.courseId ? { course_id: value.courseId } : {}),
      ...(value.lessonId ? { lesson_id: value.lessonId } : {}),
      ...(value.slideId !== undefined ? { slide_id: value.slideId } : {}),
    });
  } catch {
    /* Analytics failure never blocks bookmark opening. */
  }
}

export {
  trackCharacterRendered,
  trackCharacterRenderFallback,
  resetCharacterRenderDeduplication,
} from "../features/characters/analytics";

// E3-01 catalogue: authorized-lesson-manifest/specs/lesson-player/spec.md.
// Deliberately closed projection: no captions, titles, drafts, HTML or URLs.
export function trackPlayer(
  event:
    | "lesson_started"
    | "lesson_player_error"
    | "slide_viewed"
    | "slide_replayed"
    | "slide_paused",
  value: {
    lesson_id: string;
    course_id?: string;
    slide_id?: string;
    index?: number;
    watch_ms?: number;
    offline?: boolean;
    error_code?:
      | "manifest_unavailable"
      | "document_unavailable"
      | "invalid_manifest"
      | "document_timeout"
      | "position_unavailable"
      | "offline_uncached"
      | "cache_unavailable"
      | "checksum_mismatch"
      | "asset_unavailable"
      | "slide_failure"
      | "offline";
  },
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  const properties: Record<string, string | number | boolean> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? appVersion,
    source: "client",
  };
  for (const name of (event === "lesson_started"
    ? ["lesson_id", "course_id"]
    : ["lesson_id", "slide_id"]) as (
    "lesson_id" | "course_id" | "slide_id"
  )[]) {
    const id = value[name];
    if (id && /^[A-Za-z0-9_-]{1,100}$/.test(id)) properties[name] = id;
  }
  if (
    event === "lesson_player_error" &&
    value.error_code &&
    [
      "manifest_unavailable",
      "document_unavailable",
      "invalid_manifest",
      "document_timeout",
      "position_unavailable",
      "offline_uncached",
      "cache_unavailable",
      "checksum_mismatch",
      "asset_unavailable",
      "slide_failure",
      "offline",
    ].includes(value.error_code)
  )
    properties.error_code = value.error_code;
  if (["slide_viewed", "slide_replayed", "slide_paused"].includes(event)) {
    if (
      Number.isSafeInteger(value.index) &&
      value.index! >= 0 &&
      value.index! < 100
    )
      properties.index = value.index!;
    if (event === "slide_viewed") {
      if (Number.isSafeInteger(value.watch_ms) && value.watch_ms! >= 0)
        properties.watch_ms = value.watch_ms!;
      if (typeof value.offline === "boolean")
        properties.offline = value.offline;
    }
  }
  try {
    if (!posthog.has_opted_out_capturing()) posthog.capture(event, properties);
  } catch {
    /* Playback never depends on analytics. */
  }
}

// immersive-mobile-lesson-player analytics catalogue. Ids and closed enums only:
// no Slide text, captions, notes or timestamps finer than a bucket.
export const immersiveTriggers = [
  "control",
  "rotation",
  "back",
  "escape",
] as const;
export const immersiveControls = [
  "play",
  "pause",
  "previous",
  "next",
  "replay_slide",
  "autoplay_on",
  "autoplay_off",
  "speed",
  "captions",
  "narration",
  "slide_list",
  "bookmark",
  "lock",
  "unlock",
] as const;
export const immersiveVias = [
  "button",
  "double_tap",
  "swipe",
  "keyboard",
] as const;
export function secondsInModeBucket(seconds: number) {
  return seconds < 10
    ? "lt_10"
    : seconds < 30
      ? "10_30"
      : seconds < 120
        ? "30_120"
        : seconds < 600
          ? "120_600"
          : "gt_600";
}
export function trackImmersive(
  value:
    | {
        event: "immersive_mode_entered";
        course_id: string;
        lesson_id: string;
        slide_index: number;
        trigger: (typeof immersiveTriggers)[number];
      }
    | {
        event: "immersive_mode_exited";
        course_id: string;
        lesson_id: string;
        slide_index: number;
        trigger: (typeof immersiveTriggers)[number];
        seconds_in_mode: number;
      }
    | {
        event: "immersive_control_used";
        control: (typeof immersiveControls)[number];
        via: (typeof immersiveVias)[number];
      },
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  const properties: Record<string, string | number> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? appVersion,
    source: "client",
  };
  if (value.event === "immersive_control_used") {
    if (
      !immersiveControls.includes(value.control) ||
      !immersiveVias.includes(value.via)
    )
      return;
    properties.control = value.control;
    properties.via = value.via;
  } else {
    const id = /^[A-Za-z0-9_-]{1,100}$/;
    if (
      !id.test(value.course_id) ||
      !id.test(value.lesson_id) ||
      !immersiveTriggers.includes(value.trigger) ||
      !Number.isSafeInteger(value.slide_index) ||
      value.slide_index < 0 ||
      value.slide_index >= 100
    )
      return;
    properties.course_id = value.course_id;
    properties.lesson_id = value.lesson_id;
    properties.slide_index = value.slide_index;
    properties.trigger = value.trigger;
    if (value.event === "immersive_mode_exited")
      properties.seconds_in_mode = secondsInModeBucket(value.seconds_in_mode);
  }
  try {
    if (!posthog.has_opted_out_capturing())
      posthog.capture(value.event, properties);
  } catch {
    /* Playback never depends on analytics. */
  }
}

const areaBucket = (area: number) =>
  area < 0.08 ? "small" : area < 0.3 ? "medium" : "large";
const countBucket = (n: number) => (n <= 0 ? "0" : n === 1 ? "1" : "2_plus");

/** Region question funnel (immersive-mobile-lesson-player analytics spec): ids, counts and closed enums only. */
export function trackRegion(
  value:
    | {
        event: "lesson_region_selected";
        course_id: string;
        lesson_id: string;
        slide_index: number;
        method: "two_finger" | "select_area" | "keyboard";
        area: number;
      }
    | {
        event: "lesson_answer_panel_closed";
        course_id: string;
        lesson_id: string;
        slide_index: number;
        outcome: "completed" | "stopped" | "failed";
        saved_note: boolean;
        follow_ups: number;
      },
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  const id = /^[A-Za-z0-9_-]{1,100}$/;
  if (
    !id.test(value.course_id) ||
    !id.test(value.lesson_id) ||
    !Number.isSafeInteger(value.slide_index) ||
    value.slide_index < 0 ||
    value.slide_index >= 100
  )
    return;
  const properties: Record<string, string | number | boolean> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? appVersion,
    source: "client",
    course_id: value.course_id,
    lesson_id: value.lesson_id,
    slide_index: value.slide_index,
  };
  if (value.event === "lesson_region_selected") {
    properties.method = value.method;
    properties.area_bucket = areaBucket(value.area);
  } else {
    properties.outcome = value.outcome;
    properties.saved_note = value.saved_note;
    properties.follow_ups = countBucket(value.follow_ups);
  }
  try {
    if (!posthog.has_opted_out_capturing()) {
      posthog.capture(value.event, properties);
      // A test can opt in by defining `window.__analyticsLog = []` before the page loads.
      (
        window as unknown as { __analyticsLog?: unknown[] }
      ).__analyticsLog?.push({ event: value.event, properties });
    }
  } catch {
    /* Playback never depends on analytics. */
  }
}

export type VoiceEntryPoint = "answer_panel" | "tutor_sheet" | "quick_note";
export type VoiceErrorClass =
  | "unavailable"
  | "too_long"
  | "too_large"
  | "unsupported_container"
  | "not_entitled"
  | "rate_limited"
  | "daily_cap"
  | "failed"
  | "empty"
  | "offline";
const voiceBuckets = ["lt_2s", "2_5s", "5_15s", "15_30s"];
type VoiceCommon = {
  purpose: "tutor" | "note";
  entry_point: VoiceEntryPoint;
};
/**
 * Voice input funnel, client tier (immersive-mobile-lesson-player analytics spec). Transcription
 * outcomes are emitted by the API, which knows the served model. Closed enums only: no audio,
 * no transcript text, no ids of either. `source` is the emitting tier (client), `entry_point` the UI place.
 */
export function trackVoice(
  value: VoiceCommon &
    (
      | {
          event: "voice_recording_finished" | "voice_recording_cancelled";
          duration_bucket: string;
        }
      | {
          event: "voice_transcript_used" | "voice_transcript_discarded";
          edited: boolean;
        }
    ),
) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  if (
    ![
      "voice_recording_finished",
      "voice_recording_cancelled",
      "voice_transcript_used",
      "voice_transcript_discarded",
    ].includes(value.event) ||
    !["tutor", "note"].includes(value.purpose) ||
    !["answer_panel", "tutor_sheet", "quick_note"].includes(value.entry_point)
  )
    return;
  const properties: Record<string, string | number | boolean> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? "0.1.0",
    source: "client",
    purpose: value.purpose,
    entry_point: value.entry_point,
  };
  if ("duration_bucket" in value) {
    if (!voiceBuckets.includes(value.duration_bucket)) return;
    properties.duration_bucket = value.duration_bucket;
  }
  if ("edited" in value) properties.edited = value.edited === true;
  try {
    if (!posthog.has_opted_out_capturing())
      posthog.capture(value.event, properties);
  } catch {
    /* Dictation never depends on analytics. */
  }
}

export type VoiceQuickNoteSurface = "immersive" | "player";
export type VoiceQuickNoteFailure =
  VoiceErrorClass | "denied" | "too_short" | "save_failed";
/**
 * Voice quick note funnel (voice-quick-note analytics): closed enums, a duration and flags.
 * No transcript, title or body text, no audio, no ids.
 */
export function trackVoiceQuickNote(value: {
  event:
    | "voice_quick_note_started"
    | "voice_quick_note_saved"
    | "voice_quick_note_cancelled"
    | "voice_quick_note_failed"
    | "voice_quick_note_undone";
  surface: VoiceQuickNoteSurface;
  duration_ms?: number;
  has_region?: boolean;
  language?: string;
  reason?: VoiceQuickNoteFailure;
}) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  const properties: Record<string, string | number | boolean> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? "0.1.0",
    source: "client",
    surface: value.surface,
  };
  if (value.duration_ms !== undefined)
    properties.duration_ms = Math.max(0, Math.round(value.duration_ms));
  if (value.has_region !== undefined) properties.has_region = value.has_region;
  if (value.language && /^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$/.test(value.language))
    properties.language = value.language;
  if (value.event === "voice_quick_note_failed") {
    if (!value.reason || !/^[a-z_]{3,20}$/.test(value.reason)) return;
    properties.reason = value.reason;
  }
  try {
    if (!posthog.has_opted_out_capturing()) {
      posthog.capture(value.event, properties);
      (
        window as unknown as { __analyticsLog?: unknown[] }
      ).__analyticsLog?.push({ event: value.event, properties });
    }
  } catch {
    /* Notes never depend on analytics. */
  }
}

// E14-01 catalogue: narrate-one-slide-mock-speech/specs/analytics/spec.md. Ids and a
// duration only; narration text, audio and URLs never leave the device.
export function trackNarrationPlayed(value: {
  lesson_id: string;
  slide_id: string;
  duration_ms: number;
}) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  const id = /^[A-Za-z0-9_-]{1,100}$/;
  if (!id.test(value.lesson_id) || !id.test(value.slide_id)) return;
  const properties: Record<string, string | number> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? appVersion,
    source: "client",
    lesson_id: value.lesson_id,
    slide_id: value.slide_id,
  };
  if (Number.isSafeInteger(value.duration_ms) && value.duration_ms > 0)
    properties.duration_ms = value.duration_ms;
  try {
    if (!posthog.has_opted_out_capturing())
      posthog.capture("narration_played", properties);
  } catch {
    /* Playback never depends on analytics. */
  }
}

// E14-03 catalogue: synchronize-audio-captions-and-offline-playback/specs/analytics/spec.md
export function trackNarrationMuted(value: {
  lesson_id: string;
  slide_id: string;
  muted: boolean;
}) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  const id = /^[A-Za-z0-9_-]{1,100}$/;
  if (!id.test(value.lesson_id) || !id.test(value.slide_id)) return;
  const properties: Record<string, string | boolean> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? appVersion,
    source: "client",
    lesson_id: value.lesson_id,
    slide_id: value.slide_id,
    muted: Boolean(value.muted),
  };
  try {
    if (!posthog.has_opted_out_capturing())
      posthog.capture("narration_muted", properties);
  } catch {
    /* Playback never depends on analytics. */
  }
}

export function trackNarrationSpeedChanged(value: {
  lesson_id: string;
  slide_id: string;
  speed: number;
}) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  const id = /^[A-Za-z0-9_-]{1,100}$/;
  if (!id.test(value.lesson_id) || !id.test(value.slide_id)) return;
  if (![0.75, 1, 1.25, 1.5, 2].includes(value.speed)) return;
  const properties: Record<string, string | number> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? appVersion,
    source: "client",
    lesson_id: value.lesson_id,
    slide_id: value.slide_id,
    speed: value.speed,
  };
  try {
    if (!posthog.has_opted_out_capturing())
      posthog.capture("narration_speed_changed", properties);
  } catch {
    /* Playback never depends on analytics. */
  }
}

export function trackCaptionsToggled(value: {
  lesson_id: string;
  slide_id: string;
  captions: boolean;
}) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  const id = /^[A-Za-z0-9_-]{1,100}$/;
  if (!id.test(value.lesson_id) || !id.test(value.slide_id)) return;
  const properties: Record<string, string | boolean> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? appVersion,
    source: "client",
    lesson_id: value.lesson_id,
    slide_id: value.slide_id,
    captions: Boolean(value.captions),
  };
  try {
    if (!posthog.has_opted_out_capturing())
      posthog.capture("captions_toggled", properties);
  } catch {
    /* Playback never depends on analytics. */
  }
}

// Registered by the E3-03 cache recovery and E3-05 static recovery catalogue before emitters.
export function trackPlayerRecovery(
  action: "retry" | "exit",
  errorCode: string = "offline_uncached",
) {
  if (action !== "retry" && action !== "exit") return;
  captureAuth("ui_recovery_selected", {
    surface: "lesson_player",
    error_code: errorCode,
    action,
  });
}

// first-run-onboarding-tour catalogue: authored enums only, never learner text.
type OnboardingEvent =
  | { event: "onboarding_welcome_shown" | "onboarding_tour_completed" }
  | {
      event: "onboarding_path_chosen";
      path: "topic" | "setup_chat" | "example" | "skip";
    }
  | { event: "onboarding_tour_started"; entry_point: "first_run" | "settings" }
  | { event: "onboarding_tour_step_viewed"; step: string }
  | { event: "onboarding_tour_skipped"; at_step: string };
export function trackOnboarding(value: OnboardingEvent) {
  const steps = [
    "path",
    "next_lesson",
    "credits",
    "progress",
    "notes",
    "courses",
    "settings",
  ];
  switch (value.event) {
    case "onboarding_welcome_shown":
    case "onboarding_tour_completed":
      captureAuth(value.event, {});
      break;
    case "onboarding_path_chosen":
      if (["topic", "setup_chat", "example", "skip"].includes(value.path))
        captureAuth(value.event, { path: value.path });
      break;
    case "onboarding_tour_started":
      if (["first_run", "settings"].includes(value.entry_point))
        captureAuth(value.event, { entry_point: value.entry_point });
      break;
    case "onboarding_tour_step_viewed":
      if (steps.includes(value.step))
        captureAuth(value.event, { step: value.step });
      break;
    case "onboarding_tour_skipped":
      if (steps.includes(value.at_step))
        captureAuth(value.event, { at_step: value.at_step });
      break;
  }
}

const guidedChatEvents = [
  "guided_chat_started",
  "guided_chat_resumed",
  "guided_chat_step_answered",
  "guided_chat_step_edited",
  "guided_chat_free_text_asked",
  "guided_chat_abandoned",
  "guided_chat_completed",
] as const;
const guidedRoutes = [
  "answer",
  "question",
  "faq",
  "command",
  "off_topic",
  "fallback",
] as const;
const guidedId = /^[a-z][a-z0-9_.]{0,63}$/;
export type GuidedChatAnalytics = {
  event: (typeof guidedChatEvents)[number];
  scriptId: string;
  entryPoint: string;
  stepId?: string;
  widgetType?: string;
  skipped?: boolean;
  route?: (typeof guidedRoutes)[number];
  charged?: boolean;
  durationMs?: number;
  freeTextCount?: number;
};
/** Content-free guided chat events: ids and enums only, never answers, text or secrets. */
export function trackGuidedChatEvent(value: GuidedChatAnalytics) {
  if (!identity || identity.optedOut || !uuid.test(identity.learnerId)) return;
  if (!guidedChatEvents.includes(value.event)) return;
  const properties: Record<string, string | number | boolean> = {
    distinct_id: identity.learnerId,
    platform: identity.platform,
    app_version: identity.appVersion ?? appVersion,
    source: "client",
  };
  const ids: [string, string | undefined][] = [
    ["script_id", value.scriptId],
    ["entry_point", value.entryPoint],
    ["step_id", value.stepId],
    ["widget_type", value.widgetType?.replace("+", "_")],
  ];
  for (const [name, id] of ids) {
    if (id === undefined) continue;
    if (!guidedId.test(id)) return;
    properties[name] = id;
  }
  if (value.skipped !== undefined) properties.skipped = value.skipped;
  if (value.charged !== undefined) properties.charged = value.charged;
  if (value.route && guidedRoutes.includes(value.route))
    properties.route = value.route;
  if (value.durationMs !== undefined) {
    const s = value.durationMs / 1000;
    properties.duration_ms =
      s < 10 ? 10_000 : s < 60 ? 60_000 : s < 300 ? 300_000 : 600_000;
  }
  if (value.freeTextCount !== undefined)
    properties.free_text_count = Math.min(
      40,
      Math.max(0, Math.floor(value.freeTextCount)),
    );
  try {
    if (!posthog.has_opted_out_capturing())
      posthog.capture(value.event, properties);
  } catch {
    // UI works even when analytics is unavailable.
  }
}
