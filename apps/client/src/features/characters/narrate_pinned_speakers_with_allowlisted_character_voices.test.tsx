import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import posthog from "posthog-js/dist/module.no-external";
import { configureAnalytics } from "../../lib/analytics";
import { session } from "../accounts/session";
import { isManifest, type ManifestSlide } from "../player/manifest";
import { SegmentNarration } from "../narration/SegmentNarration";
import {
  failedSegmentIndexes,
  locateSegment,
  narratedDuration,
  playableSegments,
  segmentSlideId,
} from "./narration";
import type { SegmentNarration as SegmentState, SlideSegment } from "./types";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));

const lines = ["Look closely.", "Earth turns."];
const ready = (index: number, voice: string, duration = 650): SegmentState => ({
  status: "ready",
  audio_url: `/v1/lessons/lesson-1/slides/sky-1__s${index}/narration/audio`,
  alignment_url: `/v1/lessons/lesson-1/slides/sky-1__s${index}/narration/alignment`,
  duration_ms: duration,
  characters: 12,
  voice_key: voice,
});
const segment = (index: number, narration?: SegmentState): SlideSegment => ({
  speaker_key: index % 2 === 0 ? "guide" : "supporter",
  speaker_label: index % 2 === 0 ? "Glint" : "Pip",
  text: lines[index % 2],
  expression: "happy",
  start_ms: index * 1000,
  duration_ms: 1000,
  narration,
});
const slide = (segments: SlideSegment[], status = "ready"): ManifestSlide => ({
  html_sha256: "a".repeat(64),
  asset_sha256: {},
  slide_id: "sky-1",
  duration_ms: 3000,
  html_url: "/v1/lessons/lesson-1/slides/sky-1",
  assets: [],
  checkpoints: [],
  narration_status: status as ManifestSlide["narration_status"],
  narration_text: "",
  segments,
});
const alignmentFor = (text: string) => ({
  version: 1,
  duration_ms: text.length * 50,
  characters: [...text],
  start_ms: [...text].map((_, i) => i * 50),
  end_ms: [...text].map((_, i) => (i + 1) * 50),
});

const speakers = (name: string) => ({
  speaker_key: name,
  role: name,
  character_id: `opaque-${name}`,
  character_version: 1,
  spec: {},
});
const manifest = (s: ManifestSlide) => ({
  lesson_id: "lesson-1",
  course_id: "course-1",
  title: "Sky",
  version: 1,
  asset_origin: "https://assets.example.test",
  cast: {
    speakers: { guide: speakers("guide"), supporter: speakers("supporter") },
  },
  slides: [s],
});

describe("segment narration timeline", () => {
  const two = slide([
    segment(0, ready(0, "voice-provisional-guide", 650)),
    segment(1, { status: "failed", reason: "engine_unavailable" }),
    segment(2, ready(2, "voice-provisional-guide", 650)),
  ]);
  it("keeps speaker order, skips captions-only segments and sums the audio", () => {
    const plays = playableSegments(two);
    expect(plays.map((p) => [p.index, p.offset_ms])).toEqual([
      [0, 0],
      [2, 650],
    ]);
    expect(narratedDuration(two)).toBe(1300);
    expect(failedSegmentIndexes(two)).toEqual([1]);
    expect(segmentSlideId("sky-1", 2)).toBe("sky-1__s2");
  });
  it("seeks to the segment and offset that cover a time, clamping at both ends", () => {
    const plays = playableSegments(two);
    expect(locateSegment(plays, 700)?.play.index).toBe(2);
    expect(locateSegment(plays, 700)?.within_ms).toBe(50);
    expect(locateSegment(plays, -5)?.within_ms).toBe(0);
    expect(locateSegment(plays, 99999)?.within_ms).toBe(650);
    expect(locateSegment([], 10)).toBeNull();
  });
});

describe("manifest validation of segment narration", () => {
  const good = slide([
    segment(0, ready(0, "voice-provisional-guide")),
    segment(1, ready(1, "voice-provisional-supporter")),
  ]);
  it("accepts ready segments with their own routes and no slide-level audio", () => {
    expect(isManifest(manifest(good))).toBe(true);
  });
  it("rejects foreign routes, unknown fields, provider ids and a ready slide without ready segments", () => {
    const withNarration = (n: unknown) =>
      isManifest(
        manifest(
          slide([{ ...segment(0), narration: n as SegmentState }], "failed"),
        ),
      );
    expect(
      withNarration({ ...ready(0, "v"), audio_url: "https://evil.test/a.wav" }),
    ).toBe(false);
    expect(
      withNarration({
        ...ready(0, "v"),
        alignment_url: ready(1, "v").alignment_url,
      }),
    ).toBe(false);
    expect(
      withNarration({
        ...ready(0, "v"),
        provider_voice_id: "21m00Tcm4TlvDq8ikWAM",
      }),
    ).toBe(false);
    expect(
      withNarration({ ...ready(0, "VOICE ID"), voice_key: "Not A Key!" }),
    ).toBe(false);
    expect(withNarration({ status: "mystery" })).toBe(false);
    expect(
      withNarration({ status: "failed", audio_url: ready(0, "v").audio_url }),
    ).toBe(false);
    expect(
      withNarration({ status: "failed", reason: "engine_unavailable" }),
    ).toBe(true);
    expect(isManifest(manifest(slide([segment(0)], "ready")))).toBe(false);
  });
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

describe("explicit pinned-speaker playback", () => {
  const fetched: string[] = [];
  beforeEach(() => {
    FakeAudio.instances = [];
    fetched.length = 0;
    vi.stubGlobal("Audio", FakeAudio);
    URL.createObjectURL = vi.fn(() => "blob:narration");
    URL.revokeObjectURL = vi.fn();
    vi.spyOn(session, "request").mockImplementation(async (url: string) => {
      fetched.push(url);
      const index = Number(/__s(\d)/.exec(url)?.[1] ?? 0);
      return alignmentFor(lines[index % 2]);
    });
    vi.spyOn(session, "blob").mockResolvedValue(new Blob(["RIFF"]));
    configureAnalytics({
      learnerId: "a1111111-1111-4111-8111-111111111111",
      platform: "web",
      optedOut: false,
    });
    vi.mocked(posthog.capture).mockClear();
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  const alternating = () =>
    slide([
      segment(0, ready(0, "voice-provisional-guide")),
      segment(1, ready(1, "voice-provisional-supporter")),
    ]);

  it("never autoplays, then alternates the two speakers' audio in order", async () => {
    render(
      <SegmentNarration
        lessonId="lesson-1"
        slide={alternating()}
        rate={1.25}
        muted
        captions
      />,
    );
    expect(
      screen.getByText("Narration is ready. Press Play when you are."),
    ).toBeTruthy();
    expect(FakeAudio.instances).toHaveLength(0);
    // Captions are readable before any audio, in speaker order, with each segment's voice recorded.
    const items = [...document.querySelectorAll(".narration-segment")];
    expect(items.map((i) => i.getAttribute("data-voice"))).toEqual([
      "voice-provisional-guide",
      "voice-provisional-supporter",
    ]);
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    await screen.findByRole("button", { name: "Pause narration" });
    expect(FakeAudio.instances).toHaveLength(1);
    expect(FakeAudio.instances[0].playbackRate).toBe(1.25);
    expect(FakeAudio.instances[0].muted).toBe(true);
    await act(async () => FakeAudio.instances[0].listeners.ended());
    expect(FakeAudio.instances).toHaveLength(2);
    expect(FakeAudio.instances[1].play).toHaveBeenCalled();
    expect(fetched[0]).toContain("sky-1__s0");
    expect(fetched.some((u) => u.includes("sky-1__s1"))).toBe(true);
    expect(
      document
        .querySelector('[aria-current="true"]')
        ?.getAttribute("data-speaker"),
    ).toBe("supporter");
    // Narration played reports ids and duration only, never dialogue.
    const calls = vi
      .mocked(posthog.capture)
      .mock.calls.filter(([n]) => n === "narration_played");
    expect(calls).toHaveLength(1);
    expect(JSON.stringify(calls[0][1])).not.toContain("Look closely");
  });

  it("keeps readable captions and offers Retry for a failed segment without charging", async () => {
    const retry = vi
      .spyOn(session, "request")
      .mockResolvedValue({ state: "pending" });
    const failed = slide(
      [
        segment(0, { status: "failed", reason: "engine_unavailable" }),
        segment(1, { status: "failed", reason: "engine_unavailable" }),
      ],
      "failed",
    );
    const onRecovered = vi.fn();
    render(
      <SegmentNarration
        lessonId="lesson-1"
        slide={failed}
        rate={1}
        muted={false}
        captions
        onRecovered={onRecovered}
      />,
    );
    expect(screen.getByText("Narration unavailable")).toBeTruthy();
    expect(screen.getByText("Look closely.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Play narration" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry narration" }));
    await vi.waitFor(() => expect(onRecovered).toHaveBeenCalled());
    expect(retry.mock.calls.map((c) => [c[0], c[1]])).toEqual([
      ["/v1/lessons/lesson-1/slides/sky-1__s0/narration/retry", "POST"],
      ["/v1/lessons/lesson-1/slides/sky-1__s1/narration/retry", "POST"],
    ]);
    expect(FakeAudio.instances).toHaveLength(0);
  });

  it("says audio is paused when credits are short and keeps text available", () => {
    const paused = slide(
      [segment(0, { status: "disabled", reason: "insufficient_credits" })],
      "disabled",
    );
    render(
      <SegmentNarration
        lessonId="lesson-1"
        slide={paused}
        rate={1}
        muted={false}
        captions
      />,
    );
    expect(screen.getByText("Narration paused")).toBeTruthy();
    expect(screen.getByText("Look closely.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Add credits" })).toBeTruthy();
  });

  it("falls back to captions only when invalid alignment arrives", async () => {
    vi.spyOn(session, "request").mockResolvedValue({
      ...alignmentFor(lines[0]),
      characters: [...lines[0]].slice(1),
    });
    render(
      <SegmentNarration
        lessonId="lesson-1"
        slide={alternating()}
        rate={1}
        muted={false}
        captions
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    await screen.findByText("Narration unavailable");
    expect(FakeAudio.instances).toHaveLength(0);
    expect(screen.getByText("Earth turns.")).toBeTruthy();
  });
});
