import { beforeEach, expect, it, vi } from "vitest";

const posthog = vi.hoisted(() => ({
  identify: vi.fn(),
  capture: vi.fn(),
  opt_in_capturing: vi.fn(),
  opt_out_capturing: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

import {
  configureAnalytics,
  setAnalyticsConsent,
  trackAppearanceChanged,
  trackHotkey,
} from "../../lib/analytics";

const id = "11111111-1111-4111-8111-111111111111";
beforeEach(() => {
  vi.clearAllMocks();
  posthog.has_opted_out_capturing.mockReturnValue(false);
  configureAnalytics({
    learnerId: id,
    optedOut: false,
    platform: "web",
    appVersion: "test",
  });
});

it("captures only stable appearance and common UUID properties", () => {
  trackAppearanceChanged("dark");
  expect(posthog.capture).toHaveBeenCalledWith("ui_appearance_changed", {
    distinct_id: id,
    platform: "web",
    app_version: "test",
    source: "client",
    mode: "dark",
  });
});

it("opts out immediately and suppresses later events", () => {
  setAnalyticsConsent(false);
  expect(posthog.opt_out_capturing).toHaveBeenCalledOnce();
  trackAppearanceChanged("light");
  trackHotkey("show_help", "learn");
  expect(posthog.capture).not.toHaveBeenCalled();
});

it("records only authored hotkey identifiers and common properties", () => {
  trackHotkey("show_help", "learn");
  expect(posthog.capture).toHaveBeenCalledWith("hotkey_used", {
    distinct_id: id,
    platform: "web",
    app_version: "test",
    source: "client",
    key_id: "show_help",
    surface: "learn",
    entry_point: "hotkey",
  });
});
