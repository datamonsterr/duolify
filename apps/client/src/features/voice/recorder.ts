/** Push-to-talk rules with no browser dependency: limits, container choice, the state machine. */
export const MIN_MS = 500;
export const MAX_MS = 30_000;
/** The API refuses more than 1 MB; 32 kbps for 30 s is about 120 KB. */
export const MAX_BYTES = 1_000_000;
/** A press shorter than this is a tap: it switches to tap-to-toggle. */
export const TAP_MS = 300;
/** Normalised peak below which a recording is treated as silence and never uploaded. */
export const LEVEL_FLOOR = 0.02;
export const EDIT_WINDOW_MS = 2000;
export const BITS_PER_SECOND = 32_000;

/** WebM/Opus first, then MP4; undefined lets the browser pick its default. */
export function pickContainer(
  isSupported: (type: string) => boolean,
): string | undefined {
  return ["audio/webm;codecs=opus", "audio/mp4"].find((type) =>
    isSupported(type),
  );
}

export type DurationBucket = "lt_2s" | "2_5s" | "5_15s" | "15_30s";
export function durationBucket(ms: number): DurationBucket {
  if (ms < 2000) return "lt_2s";
  if (ms < 5000) return "2_5s";
  if (ms < 15_000) return "5_15s";
  return "15_30s";
}

export type RecordingVerdict = "ok" | "too_short" | "too_large" | "silent";
export function evaluateRecording(r: {
  elapsedMs: number;
  bytes: number;
  peak: number;
}): RecordingVerdict {
  if (r.elapsedMs < MIN_MS) return "too_short";
  if (r.bytes > MAX_BYTES) return "too_large";
  if (r.bytes === 0 || r.peak < LEVEL_FLOOR) return "silent";
  return "ok";
}

export type RecorderState =
  | { phase: "idle" }
  | { phase: "recording"; mode: "hold" | "toggle"; startedAt: number };
export const idleState: RecorderState = { phase: "idle" };

export type RecorderEvent =
  | { type: "press"; at: number }
  | { type: "release"; at: number }
  | { type: "slide_off" }
  | { type: "escape" }
  | { type: "tick"; at: number };
export type RecorderEffect =
  | { type: "start" }
  | { type: "finish"; elapsedMs: number }
  | { type: "cancel" };

export function step(
  state: RecorderState,
  event: RecorderEvent,
): { state: RecorderState; effect?: RecorderEffect } {
  if (state.phase === "idle") {
    return event.type === "press"
      ? {
          state: { phase: "recording", mode: "hold", startedAt: event.at },
          effect: { type: "start" },
        }
      : { state };
  }
  const elapsed = (at: number) => Math.max(0, at - state.startedAt);
  switch (event.type) {
    case "release":
      if (state.mode === "toggle") return { state };
      return elapsed(event.at) < TAP_MS
        ? { state: { ...state, mode: "toggle" } }
        : {
            state: idleState,
            effect: { type: "finish", elapsedMs: elapsed(event.at) },
          };
    case "press":
      return state.mode === "toggle"
        ? {
            state: idleState,
            effect: { type: "finish", elapsedMs: elapsed(event.at) },
          }
        : { state };
    case "slide_off":
      return state.mode === "hold"
        ? { state: idleState, effect: { type: "cancel" } }
        : { state };
    case "escape":
      return { state: idleState, effect: { type: "cancel" } };
    case "tick":
      return elapsed(event.at) >= MAX_MS
        ? { state: idleState, effect: { type: "finish", elapsedMs: MAX_MS } }
        : { state };
  }
}
