import { afterEach, expect, it, vi } from "vitest";
import {
  createFormatter,
  en,
  ar,
  expanded,
  resolveLocale,
} from "./localization/messages";
import {
  sanitizePreferences,
  resolveReducedMotion,
} from "./localization/Preferences";
import {
  configureUiAnalytics,
  trackAppearance,
  trackPreference,
} from "../../lib/analytics";
const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));
afterEach(() => {
  configureUiAnalytics(null);
  vi.clearAllMocks();
  posthog.has_opted_out_capturing.mockReturnValue(false);
});
it("has complete Arabic and at least 40% expanded messages under stable IDs", () => {
  expect(Object.keys(ar).sort()).toEqual(Object.keys(en).sort());
  for (const id of Object.keys(en) as (keyof typeof en)[]) {
    expect(ar[id]).toBeTruthy();
    const source = typeof en[id] === "string" ? en[id] : en[id].other;
    const target =
      typeof expanded[id] === "string" ? expanded[id] : expanded[id].other;
    expect(target.length).toBeGreaterThanOrEqual(
      Math.ceil(source.length * 1.4),
    );
  }
  expect(resolveLocale("ar-EG")).toBe("ar");
  expect(resolveLocale("not_a_locale")).toBe("en");
});
it("uses native number, currency, date, plural and bidi-safe named arguments", () => {
  const f = createFormatter("ar");
  expect(f.number(1234.5)).toBe(new Intl.NumberFormat("ar").format(1234.5));
  expect(f.currency(12.5, "USD")).toBe(
    new Intl.NumberFormat("ar", { style: "currency", currency: "USD" }).format(
      12.5,
    ),
  );
  const date = new Date("2026-11-09T23:00:00Z");
  expect(f.date(date, "Asia/Ho_Chi_Minh")).toBe(
    new Intl.DateTimeFormat("ar", {
      dateStyle: "medium",
      timeZone: "Asia/Ho_Chi_Minh",
    }).format(date),
  );
  for (const count of [0, 1, 2, 3, 11, 100, 1.5])
    expect(f.t("credits.count", { count })).toContain(f.number(count));
  expect(createFormatter("en").t("outline.move_up", { name: "ليلى" })).toBe(
    "Move \u2068ليلى\u2069 up",
  );
  expect(createFormatter("unknown").t("chat.send")).toBe("Send answer");
});
it("validates restored values and always chooses stricter motion", () => {
  expect(
    sanitizePreferences({
      locale: "bad",
      appearance: "red",
      reduceMotion: "false",
      draft: "private",
    }),
  ).toEqual({ locale: "en", appearance: "system", reduceMotion: false });
  expect(sanitizePreferences(null)).toEqual({
    locale: "en",
    appearance: "system",
    reduceMotion: false,
  });
  for (const system of [false, true])
    for (const user of [false, true])
      expect(resolveReducedMotion(system, user)).toBe(system || user);
});
it("delivers only catalogued appearance/preferences and common properties to PostHog", () => {
  configureUiAnalytics({
    learnerId: "10000000-0000-4000-8000-000000000001",
    platform: "web",
    appVersion: "test",
    enabled: true,
  });
  trackAppearance("dark");
  trackPreference("motion");
  expect(posthog.capture).toHaveBeenCalledWith("ui_appearance_changed", {
    distinct_id: "10000000-0000-4000-8000-000000000001",
    platform: "web",
    app_version: "test",
    source: "client",
    mode: "dark",
  });
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_control_activated",
    expect.objectContaining({ surface: "settings", control_id: "motion" }),
  );
  posthog.capture.mockClear();
  trackAppearance("private text" as "dark");
  trackPreference("private text" as "locale");
  expect(posthog.capture).not.toHaveBeenCalled();
  posthog.has_opted_out_capturing.mockReturnValue(true);
  trackAppearance("light");
  expect(posthog.capture).not.toHaveBeenCalled();
  configureUiAnalytics({
    learnerId: "bad",
    platform: "web",
    appVersion: "test",
    enabled: true,
  });
  trackAppearance("dark");
  expect(posthog.capture).not.toHaveBeenCalled();
  configureUiAnalytics({
    learnerId: "10000000-0000-4000-8000-000000000001",
    platform: "web",
    appVersion: "test",
    enabled: false,
  });
  trackAppearance("dark");
  expect(posthog.capture).not.toHaveBeenCalled();
});
