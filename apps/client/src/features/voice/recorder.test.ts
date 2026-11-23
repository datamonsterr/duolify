import { describe, expect, it } from "vitest";
import {
  LEVEL_FLOOR,
  MAX_BYTES,
  MAX_MS,
  MIN_MS,
  TAP_MS,
  durationBucket,
  evaluateRecording,
  idleState,
  pickContainer,
  step,
  type RecorderState,
} from "./recorder";

describe("pickContainer", () => {
  it("prefers WebM/Opus, then MP4, then lets the browser choose", () => {
    expect(pickContainer(() => true)).toBe("audio/webm;codecs=opus");
    expect(pickContainer((t) => t === "audio/mp4")).toBe("audio/mp4");
    expect(pickContainer(() => false)).toBeUndefined();
  });
});

describe("durationBucket", () => {
  it("uses the closed buckets of the analytics catalogue", () => {
    expect(durationBucket(600)).toBe("lt_2s");
    expect(durationBucket(1999)).toBe("lt_2s");
    expect(durationBucket(2000)).toBe("2_5s");
    expect(durationBucket(5000)).toBe("5_15s");
    expect(durationBucket(15000)).toBe("15_30s");
    expect(durationBucket(30000)).toBe("15_30s");
  });
});

describe("evaluateRecording", () => {
  const ok = { elapsedMs: 4000, bytes: 20_000, peak: 0.3 };
  it("accepts a normal recording", () => {
    expect(evaluateRecording(ok)).toBe("ok");
  });
  it("discards under 0.5 s", () => {
    expect(evaluateRecording({ ...ok, elapsedMs: MIN_MS - 1 })).toBe(
      "too_short",
    );
  });
  it("refuses an oversize blob and a near-silent recording", () => {
    expect(evaluateRecording({ ...ok, bytes: MAX_BYTES + 1 })).toBe(
      "too_large",
    );
    expect(evaluateRecording({ ...ok, peak: LEVEL_FLOOR / 2 })).toBe("silent");
    expect(evaluateRecording({ ...ok, bytes: 0 })).toBe("silent");
  });
});

describe("push-to-talk state machine", () => {
  const recording = (mode: "hold" | "toggle", at = 1000): RecorderState => ({
    phase: "recording",
    mode,
    startedAt: at,
  });
  it("hold: press starts, a long release finishes", () => {
    const a = step(idleState, { type: "press", at: 1000 });
    expect(a.effect).toEqual({ type: "start" });
    expect(a.state).toEqual(recording("hold"));
    const b = step(a.state, { type: "release", at: 1000 + 4000 });
    expect(b.effect).toEqual({ type: "finish", elapsedMs: 4000 });
    expect(b.state).toEqual(idleState);
  });
  it("tap: a short press turns into tap-to-toggle, the next tap finishes", () => {
    const a = step(idleState, { type: "press", at: 0 });
    const b = step(a.state, { type: "release", at: TAP_MS - 1 });
    expect(b.effect).toBeUndefined();
    expect(b.state).toEqual({ ...recording("toggle", 0) });
    const c = step(b.state, { type: "press", at: 3000 });
    expect(c.effect).toEqual({ type: "finish", elapsedMs: 3000 });
    expect(c.state).toEqual(idleState);
  });
  it("sliding off cancels a hold but not a toggle recording", () => {
    const hold = step(recording("hold"), {
      type: "slide_off",
    });
    expect(hold.effect).toEqual({ type: "cancel" });
    const toggle = step(recording("toggle"), { type: "slide_off" });
    expect(toggle.effect).toBeUndefined();
    expect(toggle.state).toEqual(recording("toggle"));
  });
  it("Escape cancels in both modes", () => {
    for (const mode of ["hold", "toggle"] as const) {
      const r = step(recording(mode), { type: "escape" });
      expect(r.effect).toEqual({ type: "cancel" });
      expect(r.state).toEqual(idleState);
    }
  });
  it("stops at 30 seconds", () => {
    const early = step(recording("hold", 0), { type: "tick", at: MAX_MS - 1 });
    expect(early.effect).toBeUndefined();
    const at = step(recording("hold", 0), { type: "tick", at: MAX_MS });
    expect(at.effect).toEqual({ type: "finish", elapsedMs: MAX_MS });
    expect(at.state).toEqual(idleState);
  });
  it("ignores events while idle", () => {
    for (const type of ["slide_off", "escape"] as const)
      expect(step(idleState, { type })).toEqual({ state: idleState });
    expect(step(idleState, { type: "release", at: 5 }).effect).toBeUndefined();
  });
});
