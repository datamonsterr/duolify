import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import posthog from "posthog-js/dist/module.no-external";
import {
  configureAnalytics,
  trackNarrationMuted,
  trackNarrationSpeedChanged,
  trackCaptionsToggled,
} from "../../lib/analytics";
import { session } from "../accounts/session";
import type { ManifestSlide } from "../player/manifest";
import { NarrationControl } from "./NarrationControl";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));

const text = "The sky is a map waiting to be explored.";
const alignment = () => ({
  version: 1,
  duration_ms: text.length * 50,
  characters: [...text],
  start_ms: [...text].map((_, i) => i * 50),
  end_ms: [...text].map((_, i) => (i + 1) * 50),
});

const slide = (over: Partial<ManifestSlide> = {}): ManifestSlide => ({
  html_sha256: "a".repeat(64),
  asset_sha256: {},
  slide_id: "sky-1",
  duration_ms: 3000,
  html_url: "/v1/lessons/lesson-1/slides/sky-1",
  assets: [],
  checkpoints: [],
  narration_status: "ready",
  narration: {
    audio_url: "/v1/lessons/lesson-1/slides/sky-1/narration/audio",
    alignment_url: "/v1/lessons/lesson-1/slides/sky-1/narration/alignment",
    duration_ms: text.length * 50,
    characters: text.length,
  },
  narration_text: text,
  ...over,
});

class FakeAudio {
  static instances: FakeAudio[] = [];
  paused = true;
  currentTime = 0;
  playbackRate = 1;
  muted = false;
  listeners: Record<string, () => void> = {};
  play = vi.fn(async () => {
    this.paused = false;
  });
  pause = vi.fn(() => {
    this.paused = true;
  });
  constructor(public src: string) {
    FakeAudio.instances.push(this);
  }
  addEventListener(name: string, fn: () => void) {
    this.listeners[name] = fn;
  }
  removeEventListener(name: string) {
    delete this.listeners[name];
  }
}

describe("narration analytics event emitters and opt-out", () => {
  const learnerId = "a1111111-1111-4111-8111-111111111111";

  beforeEach(() => {
    vi.mocked(posthog.capture).mockClear();
    configureAnalytics({
      learnerId,
      platform: "web",
      optedOut: false,
      appVersion: "0.1.0",
    });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("emits narration_muted with UUID and common properties, no private text", () => {
    trackNarrationMuted({
      lesson_id: "lesson-1",
      slide_id: "sky-1",
      muted: true,
    });
    expect(posthog.capture).toHaveBeenCalledWith("narration_muted", {
      distinct_id: learnerId,
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      lesson_id: "lesson-1",
      slide_id: "sky-1",
      muted: true,
    });
  });

  it("emits narration_speed_changed with allowed speeds only", () => {
    trackNarrationSpeedChanged({
      lesson_id: "lesson-1",
      slide_id: "sky-1",
      speed: 1.25,
    });
    expect(posthog.capture).toHaveBeenCalledWith("narration_speed_changed", {
      distinct_id: learnerId,
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      lesson_id: "lesson-1",
      slide_id: "sky-1",
      speed: 1.25,
    });
  });

  it("emits captions_toggled with boolean state", () => {
    trackCaptionsToggled({
      lesson_id: "lesson-1",
      slide_id: "sky-1",
      captions: false,
    });
    expect(posthog.capture).toHaveBeenCalledWith("captions_toggled", {
      distinct_id: learnerId,
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      lesson_id: "lesson-1",
      slide_id: "sky-1",
      captions: false,
    });
  });

  it("drops all narration telemetry when opted out", () => {
    configureAnalytics({
      learnerId,
      platform: "web",
      optedOut: true,
      appVersion: "0.1.0",
    });
    trackNarrationMuted({
      lesson_id: "lesson-1",
      slide_id: "sky-1",
      muted: true,
    });
    trackNarrationSpeedChanged({
      lesson_id: "lesson-1",
      slide_id: "sky-1",
      speed: 1.5,
    });
    trackCaptionsToggled({
      lesson_id: "lesson-1",
      slide_id: "sky-1",
      captions: true,
    });
    expect(posthog.capture).not.toHaveBeenCalled();
  });
});

describe("seek, speed, mute, and missing-audio fallback boundary cases", () => {
  beforeEach(() => {
    FakeAudio.instances = [];
    vi.stubGlobal("Audio", FakeAudio);
    URL.createObjectURL = vi.fn(() => "blob:narration");
    URL.revokeObjectURL = vi.fn();
    vi.spyOn(session, "request").mockResolvedValue(alignment());
    vi.spyOn(session, "blob").mockResolvedValue(new Blob(["RIFF"]));
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("mutes audio while preserving captions display", async () => {
    const { rerender } = render(
      <NarrationControl
        lessonId="lesson-1"
        slide={slide()}
        rate={1}
        muted={false}
        captions={true}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    await screen.findByRole("button", { name: "Pause narration" });
    expect(FakeAudio.instances[0].muted).toBe(false);
    expect(document.querySelector(".narration-caption")).toBeTruthy();

    // Toggle mute
    rerender(
      <NarrationControl
        lessonId="lesson-1"
        slide={slide()}
        rate={1}
        muted={true}
        captions={true}
      />,
    );
    expect(FakeAudio.instances[0].muted).toBe(true);
    // Captions remain visible when muted
    expect(document.querySelector(".narration-caption")).toBeTruthy();
  });

  it("applies speed changes to audio element", async () => {
    const { rerender } = render(
      <NarrationControl
        lessonId="lesson-1"
        slide={slide()}
        rate={1}
        muted={false}
        captions={true}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    await screen.findByRole("button", { name: "Pause narration" });
    expect(FakeAudio.instances[0].playbackRate).toBe(1);

    rerender(
      <NarrationControl
        lessonId="lesson-1"
        slide={slide()}
        rate={1.5}
        muted={false}
        captions={true}
      />,
    );
    expect(FakeAudio.instances[0].playbackRate).toBe(1.5);
  });

  it("falls back to caption-only display on missing or failed audio", async () => {
    vi.spyOn(session, "blob").mockRejectedValue(new Error("network error"));
    render(
      <NarrationControl
        lessonId="lesson-1"
        slide={slide()}
        rate={1}
        muted={false}
        captions={true}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    await screen.findByText("Narration unavailable");
    expect(
      screen.getByText(
        "Read the captions and transcript, then continue. The animation still plays.",
      ),
    ).toBeTruthy();
  });
});
