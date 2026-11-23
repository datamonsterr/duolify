import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ upload: vi.fn() }));
vi.mock("../accounts/session", async (original) => ({
  ...(await original<typeof import("../accounts/session")>()),
  session: { upload: mocks.upload },
}));
import { AuthError } from "../accounts/session";
import {
  capResetsAt,
  classifyError,
  recordDailyCap,
  transcribe,
  VoiceError,
} from "./api";

const blob = new Blob([new Uint8Array(10)], { type: "audio/webm" });

beforeEach(() => {
  mocks.upload.mockReset();
  localStorage.clear();
});

describe("transcribe", () => {
  it("posts multipart audio, purpose, lesson and the measured duration", async () => {
    mocks.upload.mockResolvedValue({
      text: "hello",
      language: "en",
      duration_ms: 4000,
      served_model: "qwen/qwen3-asr-0.6b",
      fallback_used: false,
    });
    const result = await transcribe({
      blob,
      purpose: "tutor",
      lessonId: "11111111-1111-4111-8111-111111111111",
      durationMs: 4123.7,
    });
    expect(result.text).toBe("hello");
    const [path, form] = mocks.upload.mock.calls[0] as [string, FormData];
    expect(path).toBe("/v1/voice/transcriptions");
    expect(form.get("purpose")).toBe("tutor");
    expect(form.get("lesson_id")).toBe("11111111-1111-4111-8111-111111111111");
    expect(form.get("duration_ms")).toBe("4124");
    expect((form.get("audio") as File).size).toBe(10);
  });
  it("turns typed API errors into classes", async () => {
    const cases: [number, unknown, string][] = [
      [503, { error: "voice_unavailable" }, "unavailable"],
      [400, { error: "voice_too_long" }, "too_long"],
      [413, { error: "voice_too_large" }, "too_large"],
      [415, { error: "voice_unsupported_container" }, "unsupported_container"],
      [403, { error: "voice_not_entitled" }, "not_entitled"],
      [429, { error: "voice_rate_limited" }, "rate_limited"],
      [502, { error: "voice_failed" }, "failed"],
      [500, undefined, "failed"],
    ];
    for (const [status, details, cls] of cases) {
      mocks.upload.mockRejectedValueOnce(new AuthError("x", status, details));
      await expect(
        transcribe({ blob, purpose: "note", lessonId: "l", durationMs: 1000 }),
      ).rejects.toMatchObject({ cls });
    }
  });
  it("remembers the daily cap until it resets and keeps typing available", async () => {
    const resets = new Date(Date.now() + 3_600_000).toISOString();
    mocks.upload.mockRejectedValueOnce(
      new AuthError("x", 429, { code: "voice_daily_cap", resets_at: resets }),
    );
    const error = await transcribe({
      blob,
      purpose: "tutor",
      lessonId: "l",
      durationMs: 1000,
    }).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(VoiceError);
    expect((error as VoiceError).cls).toBe("daily_cap");
    expect(capResetsAt()?.toISOString()).toBe(resets);
    localStorage.clear();
    recordDailyCap(new Date(Date.now() - 1000).toISOString());
    expect(capResetsAt()).toBeUndefined();
  });
  it("classifies a network failure as failed, not as a cap", () => {
    expect(classifyError(new TypeError("Failed to fetch")).cls).toBe("failed");
  });
});
