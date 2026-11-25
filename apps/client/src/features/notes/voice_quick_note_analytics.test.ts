import { beforeEach, describe, expect, it, vi } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import { configureAnalytics, trackVoiceQuickNote } from "../../lib/analytics";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));

const learnerId = "a1111111-1111-4111-8111-111111111111";

describe("voice quick note analytics", () => {
  beforeEach(() => {
    vi.mocked(posthog.capture).mockClear();
    configureAnalytics({ learnerId, platform: "ios", optedOut: false });
  });
  it("emits closed-enum properties and nothing else", () => {
    trackVoiceQuickNote({
      event: "voice_quick_note_saved",
      surface: "immersive",
      duration_ms: 1234.4,
      has_region: true,
      language: "en",
      // Text smuggled in by a caller is dropped, not sent.
      ...({ text: "secret", title: "t" } as object),
    });
    expect(posthog.capture).toHaveBeenCalledWith("voice_quick_note_saved", {
      distinct_id: learnerId,
      platform: "ios",
      app_version: "0.1.0",
      source: "client",
      surface: "immersive",
      duration_ms: 1234,
      has_region: true,
      language: "en",
    });
  });
  it("drops a failure without a known reason and a free-text language", () => {
    trackVoiceQuickNote({
      event: "voice_quick_note_failed",
      surface: "player",
    });
    trackVoiceQuickNote({
      event: "voice_quick_note_failed",
      surface: "player",
      reason: "hello world" as never,
    });
    trackVoiceQuickNote({
      event: "voice_quick_note_started",
      surface: "player",
      language: "secret transcript words",
    });
    const calls = vi.mocked(posthog.capture).mock.calls;
    expect(calls).toHaveLength(1);
    expect(calls[0][1]).not.toHaveProperty("language");
  });
});
