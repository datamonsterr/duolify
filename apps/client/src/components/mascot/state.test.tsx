import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { deriveMascotState, SLEEPY_AFTER_MS, useMascotState } from "./state";

describe("deriveMascotState", () => {
  it("maps UI signals to the eight states", () => {
    expect(deriveMascotState({})).toBe("idle");
    expect(deriveMascotState({ listening: true })).toBe("listening");
    expect(deriveMascotState({ waiting: true })).toBe("thinking");
    expect(deriveMascotState({ streaming: true })).toBe("talking");
    expect(deriveMascotState({ error: true })).toBe("confused");
    expect(deriveMascotState({}, "happy")).toBe("happy");
    expect(deriveMascotState({}, "celebrate")).toBe("celebrate");
    expect(deriveMascotState({}, null, true)).toBe("sleepy");
  });
  it("offline wins, then celebrate, then error, thinking, talking, listening", () => {
    expect(deriveMascotState({ offline: true, error: true }, "celebrate")).toBe(
      "sleepy",
    );
    expect(deriveMascotState({ error: true }, "celebrate")).toBe("celebrate");
    expect(deriveMascotState({ error: true, waiting: true })).toBe("confused");
    expect(deriveMascotState({ waiting: true, streaming: true })).toBe(
      "thinking",
    );
    expect(deriveMascotState({ streaming: true, listening: true })).toBe(
      "talking",
    );
  });
});

describe("useMascotState", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());
  it("celebrates then returns to idle", () => {
    const { result } = renderHook(() => useMascotState());
    act(() => result.current.cue("celebrate"));
    expect(result.current.state).toBe("celebrate");
    act(() => void vi.advanceTimersByTime(2700));
    expect(result.current.state).toBe("idle");
  });
  it("goes sleepy after 60 s idle and wakes on a signal", () => {
    const { result, rerender } = renderHook((p) => useMascotState(p), {
      initialProps: {} as { listening?: boolean },
    });
    act(() => void vi.advanceTimersByTime(SLEEPY_AFTER_MS + 100));
    expect(result.current.state).toBe("sleepy");
    rerender({ listening: true });
    expect(result.current.state).toBe("listening");
  });
  it("follows the signals without delay", () => {
    const { result, rerender } = renderHook((p) => useMascotState(p), {
      initialProps: { waiting: true } as {
        waiting?: boolean;
        streaming?: boolean;
      },
    });
    expect(result.current.state).toBe("thinking");
    rerender({ streaming: true });
    expect(result.current.state).toBe("talking");
  });
});
