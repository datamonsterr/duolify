import { AuthError, session } from "../accounts/session";
import type { VoiceErrorClass } from "../../lib/analytics";

export type Transcription = {
  text: string;
  /** Empty when the provider does not report one. */
  language: string;
  duration_ms: number;
  served_model: string;
  fallback_used: boolean;
};

export class VoiceError extends Error {
  constructor(
    public cls: VoiceErrorClass,
    public status = 0,
    public resetsAt?: string,
  ) {
    super(`voice: ${cls}`);
  }
}

const byCode: Record<string, VoiceErrorClass> = {
  voice_unavailable: "unavailable",
  voice_too_long: "too_long",
  voice_too_large: "too_large",
  voice_unsupported_container: "unsupported_container",
  voice_not_entitled: "not_entitled",
  voice_rate_limited: "rate_limited",
  voice_daily_cap: "daily_cap",
  voice_failed: "failed",
};

/** Map any thrown value to a typed voice error (the API's `code`, or its `error` string when that is a code). */
export function classifyError(error: unknown): VoiceError {
  if (error instanceof VoiceError) return error;
  if (error instanceof AuthError) {
    const d = (error.details ?? {}) as {
      code?: string;
      error?: string;
      resets_at?: string;
    };
    const cls =
      byCode[d.code ?? ""] ??
      byCode[d.error ?? ""] ??
      (error.status === 503 ? "unavailable" : "failed");
    return new VoiceError(cls, error.status, d.resets_at);
  }
  return new VoiceError("failed");
}

const CAP_KEY = "duolify.voice.cap-resets-at";
export function recordDailyCap(resetsAt: string | undefined) {
  try {
    if (resetsAt && Number.isFinite(Date.parse(resetsAt)))
      localStorage.setItem(CAP_KEY, resetsAt);
  } catch {
    /* The server still enforces the cap. */
  }
}
/** When the daily cap lifts, or undefined when no cap is remembered or it has passed. */
export function capResetsAt(now = Date.now()): Date | undefined {
  try {
    const raw = localStorage.getItem(CAP_KEY);
    const at = raw ? Date.parse(raw) : NaN;
    if (Number.isFinite(at) && at > now) return new Date(at);
    if (raw) localStorage.removeItem(CAP_KEY);
  } catch {
    /* ignore */
  }
  return undefined;
}

export async function transcribe(input: {
  blob: Blob;
  purpose: "tutor" | "note";
  lessonId: string;
  durationMs: number;
}): Promise<Transcription> {
  const form = new FormData();
  form.set("purpose", input.purpose);
  form.set("lesson_id", input.lessonId);
  form.set("duration_ms", String(Math.round(input.durationMs)));
  // The API identifies the container itself; the reported MIME type is only a hint.
  form.set("audio", input.blob, "recording");
  try {
    return await session.upload<Transcription>(
      "/v1/voice/transcriptions",
      form,
    );
  } catch (error) {
    const typed = classifyError(error);
    if (typed.cls === "daily_cap") recordDailyCap(typed.resetsAt);
    throw typed;
  }
}
