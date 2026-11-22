import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AUTO_HIDE_MS, canAutoHide, createAutoHide } from "./autohide";

const free = {
  focusInside: false,
  assistive: false,
  stoppedAtEnd: false,
  locked: false,
  pinned: false,
};
beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("canAutoHide", () => {
  it("hides when nothing blocks it", () =>
    expect(canAutoHide(free)).toBe(true));
  for (const k of [
    "focusInside",
    "assistive",
    "stoppedAtEnd",
    "pinned",
  ] as const)
    it(`never hides while ${k}`, () =>
      expect(canAutoHide({ ...free, [k]: true })).toBe(false));
});

describe("createAutoHide", () => {
  it("hides after 3 seconds", () => {
    const hide = vi.fn();
    const t = createAutoHide(hide);
    t.arm(true);
    vi.advanceTimersByTime(AUTO_HIDE_MS - 1);
    expect(hide).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(hide).toHaveBeenCalledOnce();
    expect(AUTO_HIDE_MS).toBe(3000);
  });
  it("any interaction restarts the countdown", () => {
    const hide = vi.fn();
    const t = createAutoHide(hide);
    t.arm(true);
    vi.advanceTimersByTime(2500);
    t.arm(true);
    vi.advanceTimersByTime(2500);
    expect(hide).not.toHaveBeenCalled();
    vi.advanceTimersByTime(600);
    expect(hide).toHaveBeenCalledOnce();
  });
  it("does not run while blocked and cancels a running timer", () => {
    const hide = vi.fn();
    const t = createAutoHide(hide);
    t.arm(true);
    t.arm(false);
    vi.advanceTimersByTime(10_000);
    expect(hide).not.toHaveBeenCalled();
  });
  it("dispose cancels", () => {
    const hide = vi.fn();
    const t = createAutoHide(hide);
    t.arm(true);
    t.dispose();
    vi.advanceTimersByTime(10_000);
    expect(hide).not.toHaveBeenCalled();
  });
});
