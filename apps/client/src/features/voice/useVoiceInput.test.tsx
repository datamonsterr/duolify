import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  pricing: vi.fn(),
  transcribe: vi.fn(),
  track: vi.fn(),
}));
vi.mock("../../lib/api", () => ({
  api: { pricing: mocks.pricing },
  readPricingCache: () => undefined,
}));
vi.mock("../../lib/analytics", () => ({ trackVoice: mocks.track }));
vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  transcribe: mocks.transcribe,
}));
import { VoiceError } from "./api";
import type { RecorderHandle, VoicePlatform } from "./platform";
import { useVoiceInput } from "./useVoiceInput";

const LESSON = "11111111-1111-4111-8111-111111111111";
let clock = 0;
const now = () => clock;

function fake(opts: { peak?: number; size?: number; deny?: boolean } = {}) {
  const handle = {
    stop: vi.fn(async () => new Blob([new Uint8Array(opts.size ?? 4000)])),
    cancel: vi.fn(),
    level: () => 0.4,
    peak: () => opts.peak ?? 0.4,
  } satisfies RecorderHandle;
  const platform: VoicePlatform = {
    supported: () => true,
    start: vi.fn(async () => {
      if (opts.deny)
        throw Object.assign(new Error("denied"), { name: "NotAllowedError" });
      return handle;
    }),
  };
  return { handle, platform };
}

function setup(platform: VoicePlatform, lessonId: string | null = LESSON) {
  const onTranscript = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const view = renderHook(
    () =>
      useVoiceInput({
        purpose: "tutor",
        entryPoint: "answer_panel",
        lessonId: lessonId ?? undefined,
        onTranscript,
        platform,
        now,
      }),
    { wrapper },
  );
  return { ...view, onTranscript };
}
const events = () => mocks.track.mock.calls.map((c) => c[0].event);
const hold = async (
  r: { current: ReturnType<typeof useVoiceInput> },
  ms: number,
) => {
  await act(async () => {
    r.current.press();
  });
  clock += ms;
  await act(async () => {
    r.current.release();
  });
};

beforeEach(() => {
  clock = 1000;
  localStorage.clear();
  Object.defineProperty(navigator, "onLine", {
    value: true,
    configurable: true,
  });
  mocks.pricing.mockResolvedValue({ transcribe: { daily_requests: 60 } });
  mocks.transcribe.mockResolvedValue({
    text: " What is a closure? ",
    language: "en",
    duration_ms: 4000,
    served_model: "qwen/qwen3-asr-0.6b",
    fallback_used: false,
  });
  mocks.track.mockClear();
});
afterEach(() => vi.clearAllMocks());

describe("useVoiceInput", () => {
  it("hold, release, transcribe: the transcript reaches the composer and events are emitted", async () => {
    const { handle, platform } = fake();
    const { result, onTranscript } = setup(platform);
    await waitFor(() => expect(result.current.block).toBeUndefined());
    await hold(result, 4000);
    await waitFor(() => expect(onTranscript).toHaveBeenCalled());
    expect(onTranscript).toHaveBeenCalledWith({
      text: "What is a closure?",
      language: "en",
    });
    expect(mocks.transcribe).toHaveBeenCalledWith(
      expect.objectContaining({
        purpose: "tutor",
        lessonId: LESSON,
        durationMs: 4000,
      }),
    );
    expect(handle.stop).toHaveBeenCalled();
    // Transcription outcomes are emitted by the API, not the client.
    expect(events()).toEqual(["voice_recording_finished"]);
    // Event payloads never carry the transcript.
    expect(JSON.stringify(mocks.track.mock.calls)).not.toContain("closure");
  });

  it("sliding off cancels, uploads nothing and releases the microphone", async () => {
    const { handle, platform } = fake();
    const { result } = setup(platform);
    await waitFor(() => expect(result.current.block).toBeUndefined());
    await act(async () => result.current.press());
    clock += 2000;
    await act(async () => result.current.slideOff());
    expect(handle.cancel).toHaveBeenCalled();
    expect(mocks.transcribe).not.toHaveBeenCalled();
    expect(events()).toEqual(["voice_recording_cancelled"]);
    expect(result.current.phase).toBe("idle");
  });

  it("a tap toggles recording on and a second tap finishes it", async () => {
    const { platform } = fake();
    const { result, onTranscript } = setup(platform);
    await waitFor(() => expect(result.current.block).toBeUndefined());
    await hold(result, 100);
    expect(result.current.phase).toBe("recording");
    clock += 3000;
    await act(async () => result.current.press());
    await waitFor(() => expect(onTranscript).toHaveBeenCalled());
    expect(mocks.transcribe).toHaveBeenCalledWith(
      expect.objectContaining({ durationMs: 3100 }),
    );
  });

  it("discards a recording under 0.5 s and a silent one without uploading", async () => {
    const short = fake();
    const a = setup(short.platform);
    await waitFor(() => expect(a.result.current.block).toBeUndefined());
    await hold(a.result, 400);
    await waitFor(() =>
      expect(a.result.current.notice?.kind).toBe("too_short"),
    );
    const silent = fake({ peak: 0.001 });
    const b = setup(silent.platform);
    await waitFor(() => expect(b.result.current.block).toBeUndefined());
    await hold(b.result, 3000);
    await waitFor(() => expect(b.result.current.notice?.kind).toBe("silent"));
    expect(mocks.transcribe).not.toHaveBeenCalled();
  });

  it("stops at 30 seconds and transcribes what was captured", async () => {
    vi.useFakeTimers();
    try {
      const { platform } = fake();
      const { result, onTranscript } = setup(platform);
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10);
      });
      await act(async () => result.current.press());
      for (let i = 0; i < 80; i++) {
        clock += 400;
        await act(async () => {
          await vi.advanceTimersByTimeAsync(400);
        });
      }
      vi.useRealTimers();
      await waitFor(() => expect(onTranscript).toHaveBeenCalled());
      expect(mocks.transcribe).toHaveBeenCalledWith(
        expect.objectContaining({ durationMs: 30_000 }),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it("permission denied explains itself and creates nothing", async () => {
    const { platform } = fake({ deny: true });
    const { result } = setup(platform);
    await waitFor(() => expect(result.current.block).toBeUndefined());
    await act(async () => result.current.press());
    await waitFor(() => expect(result.current.notice?.kind).toBe("denied"));
    expect(result.current.block).toBe("denied");
    expect(mocks.transcribe).not.toHaveBeenCalled();
  });

  it("is blocked offline, without a transcription rate, and without a Lesson", async () => {
    Object.defineProperty(navigator, "onLine", {
      value: false,
      configurable: true,
    });
    expect(setup(fake().platform).result.current.block).toBe("offline");
    Object.defineProperty(navigator, "onLine", {
      value: true,
      configurable: true,
    });
    mocks.pricing.mockResolvedValue({ transcribe: null });
    const off = setup(fake().platform);
    await waitFor(() => expect(off.result.current.block).toBe("unavailable"));
    mocks.pricing.mockResolvedValue({ transcribe: { daily_requests: 60 } });
    expect(setup(fake().platform, null).result.current.block).toBe("no_lesson");
  });

  it("shows the cap, remembers it, and keeps nothing to retry", async () => {
    mocks.transcribe.mockRejectedValue(
      new VoiceError(
        "daily_cap",
        429,
        new Date(Date.now() + 3_600_000).toISOString(),
      ),
    );
    localStorage.setItem(
      "duolify.voice.cap-resets-at",
      new Date(Date.now() + 3_600_000).toISOString(),
    );
    const { result } = setup(fake().platform);
    await waitFor(() => expect(result.current.block).toBe("daily_cap"));
    expect(result.current.capResets).toBeInstanceOf(Date);
  });

  it("an empty transcript and a failed request offer retry and create nothing", async () => {
    mocks.transcribe.mockResolvedValueOnce({
      text: "  ",
      language: "",
      duration_ms: 3000,
      served_model: "qwen/qwen3-asr-0.6b",
      fallback_used: false,
    });
    const { platform } = fake();
    const { result, onTranscript } = setup(platform);
    await waitFor(() => expect(result.current.block).toBeUndefined());
    await hold(result, 3000);
    await waitFor(() => expect(result.current.notice?.kind).toBe("empty"));
    expect(onTranscript).not.toHaveBeenCalled();
    mocks.transcribe.mockRejectedValueOnce(new VoiceError("failed", 502));
    await act(async () => result.current.retry());
    await waitFor(() =>
      expect(result.current.notice).toEqual({
        kind: "error",
        cls: "failed",
        retry: true,
      }),
    );
    mocks.transcribe.mockResolvedValueOnce({
      text: "hi",
      language: "en",
      duration_ms: 3000,
      served_model: "m/x",
      fallback_used: true,
    });
    await act(async () => result.current.retry());
    await waitFor(() =>
      expect(onTranscript).toHaveBeenCalledWith({ text: "hi", language: "en" }),
    );
  });
});
