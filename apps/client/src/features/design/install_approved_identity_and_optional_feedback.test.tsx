import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import {
  determineHapticPattern,
  shouldPlaySound,
  triggerFeedbackCue,
  setFeedbackSettings,
  readFeedbackSettings,
} from "./feedback/feedbackBridge";
import { configureUiAnalytics, trackPreference } from "../../lib/analytics";

const telemetry = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));

vi.mock("posthog-js/dist/module.no-external", () => ({ default: telemetry }));

describe("install_approved_identity_and_optional_feedback unit tests", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    configureUiAnalytics(null);
  });

  describe("Feedback cue boundary cases", () => {
    it("determines sound playback is always a silent no-op in v1 even when enabled", () => {
      // With no audio assets shipped in v1 (ADR 0013), shouldPlaySound returns false or no-op
      expect(shouldPlaySound({ soundEnabled: true })).toBe(false);
      expect(shouldPlaySound({ soundEnabled: false })).toBe(false);
    });

    it("determines correct haptic pattern for tap when enabled", () => {
      const pattern = determineHapticPattern({
        type: "tap",
        hapticsSetting: "on",
        reduceMotion: false,
      });
      // Single light tap (e.g. 15ms)
      expect(pattern).toEqual([15]);
    });

    it("determines paired pulse haptic pattern for completion when enabled", () => {
      const pattern = determineHapticPattern({
        type: "completion",
        hapticsSetting: "on",
        reduceMotion: false,
      });
      // Short paired pulse (e.g. [20, 40, 20])
      expect(pattern).toEqual([20, 40, 20]);
    });

    it("keeps paired pulse haptic pattern for completion even when reduce motion is enabled", () => {
      const pattern = determineHapticPattern({
        type: "completion",
        hapticsSetting: "on",
        reduceMotion: true,
      });
      // Reduced motion suppresses celebration movement only; haptics remain separate opt-in
      expect(pattern).toEqual([20, 40, 20]);
    });

    it("returns null pattern when haptics setting is off", () => {
      const pattern = determineHapticPattern({
        type: "tap",
        hapticsSetting: "off",
        reduceMotion: false,
      });
      expect(pattern).toBeNull();
    });

    it("never returns a punishing vibration for corrections", () => {
      const pattern = determineHapticPattern({
        type: "correction",
        hapticsSetting: "on",
        reduceMotion: false,
      });
      // No punishment / vibration on mistakes
      expect(pattern).toBeNull();
    });
  });

  describe("Settings mapping and triggerFeedbackCue", () => {
    let vibrateSpy: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      vibrateSpy = vi.fn();
      Object.defineProperty(navigator, "vibrate", {
        value: vibrateSpy,
        configurable: true,
        writable: true,
      });
    });

    it("reads saved settings defaults and updates through setFeedbackSettings", () => {
      setFeedbackSettings({ ui_sounds: true, haptics: "on" });
      const current = readFeedbackSettings();
      expect(current.ui_sounds).toBe(true);
      expect(current.haptics).toBe("on");
    });

    it("maps tap cue to one light haptic pulse when haptics=on", () => {
      setFeedbackSettings({ haptics: "on" });
      triggerFeedbackCue("tap");
      expect(vibrateSpy).toHaveBeenCalledWith([15]);
    });

    it("maps completion cue to paired pulse haptic when haptics=on", () => {
      setFeedbackSettings({ haptics: "on" });
      triggerFeedbackCue("completion");
      expect(vibrateSpy).toHaveBeenCalledWith([20, 40, 20]);
    });

    it("maps correction cue to a no-op haptic without vibrating", () => {
      setFeedbackSettings({ haptics: "on" });
      triggerFeedbackCue("correction");
      expect(vibrateSpy).not.toHaveBeenCalled();
    });

    it("suppresses all vibration when haptics=off", () => {
      setFeedbackSettings({ haptics: "off" });
      triggerFeedbackCue("tap");
      triggerFeedbackCue("completion");
      expect(vibrateSpy).not.toHaveBeenCalled();
    });
  });

  describe("Analytics event emitters for feedback settings", () => {
    it("emits ui_control_activated when opted in and learner UUID is valid", () => {
      configureUiAnalytics({
        learnerId: "10000000-0000-4000-8000-000000000001",
        enabled: true,
        platform: "web",
        appVersion: "0.1.0",
      });

      trackPreference("ui_sounds");
      expect(telemetry.capture).toHaveBeenCalledWith("ui_control_activated", {
        distinct_id: "10000000-0000-4000-8000-000000000001",
        platform: "web",
        app_version: "0.1.0",
        source: "client",
        surface: "settings",
        control_id: "ui_sounds",
      });

      trackPreference("haptics");
      expect(telemetry.capture).toHaveBeenCalledWith("ui_control_activated", {
        distinct_id: "10000000-0000-4000-8000-000000000001",
        platform: "web",
        app_version: "0.1.0",
        source: "client",
        surface: "settings",
        control_id: "haptics",
      });
    });

    it("suppresses event emission when opted out", () => {
      configureUiAnalytics({
        learnerId: "10000000-0000-4000-8000-000000000001",
        enabled: false,
        platform: "web",
        appVersion: "0.1.0",
      });

      trackPreference("ui_sounds");
      expect(telemetry.capture).not.toHaveBeenCalled();
    });

    it("suppresses event emission when learner UUID is absent or invalid", () => {
      configureUiAnalytics({
        learnerId: "not-a-valid-uuid",
        enabled: true,
        platform: "web",
        appVersion: "0.1.0",
      });

      trackPreference("ui_sounds");
      expect(telemetry.capture).not.toHaveBeenCalled();
    });
  });
});
