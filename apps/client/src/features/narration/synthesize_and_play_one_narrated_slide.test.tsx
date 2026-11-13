import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import posthog from "posthog-js/dist/module.no-external";
import { configureAnalytics } from "../../lib/analytics";
import { session } from "../accounts/session";
import { isManifest, type ManifestSlide } from "../player/manifest";
import { parseAlignment, spokenCount } from "./alignment";
import { NarrationControl } from "./NarrationControl";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));

const text = "Stars are suns.";
const alignment = (overrides: Record<string, unknown> = {}) => ({
  version: 1,
  duration_ms: text.length * 50,
  characters: [...text],
  start_ms: [...text].map((_, i) => i * 50),
  end_ms: [...text].map((_, i) => (i + 1) * 50),
  ...overrides,
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

describe("alignment validation", () => {
  it("accepts complete alignment and counts spoken characters by time", () => {
    const a = parseAlignment(alignment(), text);
    expect(a).not.toBeNull();
    expect(spokenCount(a!, 0)).toBe(1);
    expect(spokenCount(a!, 120)).toBe(3);
    expect(spokenCount(a!, 99999)).toBe(text.length);
  });
  it("rejects missing text, gaps, reversals, wrong text and duration drift", () => {
    expect(parseAlignment(alignment(), "")).toBeNull();
    expect(parseAlignment(alignment(), "Different words")).toBeNull();
    expect(
      parseAlignment(alignment({ characters: [...text].slice(1) }), text),
    ).toBeNull();
    const reversed = alignment();
    reversed.start_ms = [...reversed.start_ms];
    reversed.start_ms[3] = 0;
    expect(parseAlignment(reversed, text)).toBeNull();
    expect(parseAlignment(alignment({ duration_ms: 60000 }), text)).toBeNull();
    expect(parseAlignment({ version: 2 }, text)).toBeNull();
    expect(parseAlignment(null, text)).toBeNull();
  });
});

describe("manifest narration projection", () => {
  const manifest = (s: ManifestSlide) => ({
    lesson_id: "lesson-1",
    course_id: "course-1",
    title: "Sky",
    version: 1,
    asset_origin: "https://assets.example.test",
    slides: [s],
  });
  it("accepts ready narration for the same slide only", () => {
    expect(isManifest(manifest(slide()))).toBe(true);
    expect(
      isManifest(
        manifest(
          slide({
            narration: {
              ...slide().narration!,
              audio_url: "https://evil.test/a.wav",
            },
          }),
        ),
      ),
    ).toBe(false);
    expect(isManifest(manifest(slide({ narration: undefined })))).toBe(false);
    expect(isManifest(manifest(slide({ narration_status: "failed" })))).toBe(
      false,
    );
    expect(
      isManifest(
        manifest(slide({ narration_status: "failed", narration: undefined })),
      ),
    ).toBe(true);
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

describe("explicit narration playback", () => {
  beforeEach(() => {
    FakeAudio.instances = [];
    vi.stubGlobal("Audio", FakeAudio);
    URL.createObjectURL = vi.fn(() => "blob:narration");
    URL.revokeObjectURL = vi.fn();
    vi.spyOn(session, "request").mockResolvedValue(alignment());
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

  it("never autoplays, then plays on press with spoken captions marked", async () => {
    render(
      <NarrationControl
        lessonId="lesson-1"
        slide={slide()}
        rate={1}
        muted={false}
        captions
      />,
    );
    expect(
      screen.getByText("Narration is ready. Press Play when you are."),
    ).toBeTruthy();
    expect(FakeAudio.instances).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    const pause = await screen.findByRole("button", {
      name: "Pause narration",
    });
    expect(FakeAudio.instances).toHaveLength(1);
    expect(FakeAudio.instances[0].play).toHaveBeenCalledTimes(1);
    expect(document.querySelector(".narration-caption mark")).not.toBeNull();
    expect(document.querySelector(".narration-caption")?.textContent).toBe(
      text,
    );
    fireEvent.click(pause);
    expect(FakeAudio.instances[0].pause).toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Play narration" })).toBeTruthy();
  });

  it("emits narration_played with ids and duration only, never text", async () => {
    render(
      <NarrationControl
        lessonId="lesson-1"
        slide={slide()}
        rate={1}
        muted={false}
        captions
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    await screen.findByRole("button", { name: "Pause narration" });
    const calls = vi
      .mocked(posthog.capture)
      .mock.calls.filter(([name]) => name === "narration_played");
    expect(calls).toHaveLength(1);
    const properties = calls[0][1] as Record<string, unknown>;
    expect(properties).toMatchObject({
      lesson_id: "lesson-1",
      slide_id: "sky-1",
      duration_ms: text.length * 50,
      source: "client",
    });
    expect(JSON.stringify(properties)).not.toContain("Stars");
  });

  it("rejects invalid alignment and keeps captions-only recovery", async () => {
    vi.spyOn(session, "request").mockResolvedValue(
      alignment({ characters: [...text].slice(1) }),
    );
    render(
      <NarrationControl
        lessonId="lesson-1"
        slide={slide()}
        rate={1}
        muted={false}
        captions
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    expect(await screen.findByText("Narration unavailable")).toBeTruthy();
    expect(FakeAudio.instances).toHaveLength(0);
    expect(screen.queryByRole("button", { name: /narration$/ })).toBeNull();
    expect(
      vi
        .mocked(posthog.capture)
        .mock.calls.some(([n]) => n === "narration_played"),
    ).toBe(false);
  });

  it.each(["failed", "pending"] as const)(
    "shows unavailable guidance for %s narration without a Play control",
    (status) => {
      render(
        <NarrationControl
          lessonId="lesson-1"
          slide={slide({ narration_status: status, narration: undefined })}
          rate={1}
          muted={false}
          captions
        />,
      );
      expect(
        screen.queryByRole("button", { name: "Play narration" }),
      ).toBeNull();
      expect(
        screen.getByText(
          status === "failed"
            ? "Read the captions and transcript, then continue. The animation still plays."
            : "Narration is being prepared.",
        ),
      ).toBeTruthy();
    },
  );

  it("renders nothing for disabled narration", () => {
    const { container } = render(
      <NarrationControl
        lessonId="lesson-1"
        slide={slide({ narration_status: "disabled", narration: undefined })}
        rate={1}
        muted={false}
        captions
      />,
    );
    expect(container.textContent).toBe("");
  });

  it("applies the player speed and mute to the audio", async () => {
    render(
      <NarrationControl
        lessonId="lesson-1"
        slide={slide()}
        rate={1.5}
        muted
        captions
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    await screen.findByRole("button", { name: "Pause narration" });
    expect(FakeAudio.instances[0].playbackRate).toBe(1.5);
    expect(FakeAudio.instances[0].muted).toBe(true);
  });
});
