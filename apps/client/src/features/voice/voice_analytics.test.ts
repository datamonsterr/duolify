import { beforeEach, describe, expect, it, vi } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import { configureAnalytics, trackVoice } from "../../lib/analytics";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));

const learnerId = "a1111111-1111-4111-8111-111111111111";
const common = { purpose: "tutor", entry_point: "answer_panel" } as const;
const on = (optedOut = false) =>
  configureAnalytics({ learnerId, platform: "ios", optedOut });

describe("voice analytics", () => {
  beforeEach(() => {
    vi.mocked(posthog.capture).mockClear();
    on();
  });
  it("emits the recording events with a bucket, source=client and the entry point", () => {
    trackVoice({
      ...common,
      event: "voice_recording_finished",
      duration_bucket: "2_5s",
    });
    expect(posthog.capture).toHaveBeenCalledWith("voice_recording_finished", {
      distinct_id: learnerId,
      platform: "ios",
      app_version: "0.1.0",
      source: "client",
      purpose: "tutor",
      entry_point: "answer_panel",
      duration_bucket: "2_5s",
    });
  });
  it("never lets a bad bucket through and has no client transcription events", () => {
    trackVoice({
      ...common,
      event: "voice_recording_cancelled",
      duration_bucket: "a 7 second note" as never,
    });
    trackVoice({
      ...common,
      event: "voice_transcription_succeeded",
      duration_bucket: "lt_2s",
    } as never);
    expect(posthog.capture).not.toHaveBeenCalled();
  });
  it("emits used and discarded with the edited flag", () => {
    trackVoice({ ...common, event: "voice_transcript_used", edited: true });
    expect(posthog.capture).toHaveBeenLastCalledWith(
      "voice_transcript_used",
      expect.objectContaining({ edited: true }),
    );
    trackVoice({
      ...common,
      event: "voice_transcript_discarded",
      edited: false,
    });
    expect(posthog.capture).toHaveBeenLastCalledWith(
      "voice_transcript_discarded",
      expect.objectContaining({ edited: false }),
    );
  });
  it("emits nothing for an opted-out Learner", () => {
    on(true);
    trackVoice({
      ...common,
      event: "voice_recording_finished",
      duration_bucket: "2_5s",
    });
    expect(posthog.capture).not.toHaveBeenCalled();
  });
});
