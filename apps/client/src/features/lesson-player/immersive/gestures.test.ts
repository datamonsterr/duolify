import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  DOUBLE_TAP_MS,
  MIN_REGION,
  createGestureRecognizer,
  normalizeRect,
  type RegionRect,
} from "./gestures";

const size = { w: 844, h: 475 }; // displayed stage in CSS px
function setup(withRegion = true) {
  const calls: string[] = [];
  const regions: { phase: string; rect: RegionRect; accidental?: boolean }[] =
    [];
  const g = createGestureRecognizer(
    {
      onTap: () => calls.push("tap"),
      onDoubleTap: (side) => calls.push(`double:${side}`),
      onSwipe: (dir) => calls.push(`swipe:${dir}`),
      ...(withRegion
        ? {
            onRegionGestureStart: (rect: RegionRect) =>
              regions.push({ phase: "start", rect }),
            onRegionGestureUpdate: (rect: RegionRect) =>
              regions.push({ phase: "update", rect }),
            onRegionGestureEnd: (
              rect: RegionRect,
              info: { accidental: boolean },
            ) =>
              regions.push({ phase: "end", rect, accidental: info.accidental }),
          }
        : {}),
    },
    () => size,
  );
  return { g, calls, regions };
}
const tap = (
  g: ReturnType<typeof setup>["g"],
  id: number,
  x: number,
  y: number,
  t: number,
) => {
  g.down(id, x, y, t);
  g.up(id, x, y, t + 60);
};

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("tap and double tap", () => {
  it("waits for the double-tap interval before reporting a single tap", () => {
    const { g, calls } = setup();
    tap(g, 1, 0.5, 0.5, 0);
    expect(calls).toEqual([]);
    vi.advanceTimersByTime(DOUBLE_TAP_MS - 1);
    expect(calls).toEqual([]);
    vi.advanceTimersByTime(2);
    expect(calls).toEqual(["tap"]);
  });
  it("reports a double tap on the right half as next and no single tap", () => {
    const { g, calls } = setup();
    tap(g, 1, 0.8, 0.5, 0);
    tap(g, 2, 0.81, 0.5, 150);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual(["double:right"]);
  });
  it("reports a double tap on the left half", () => {
    const { g, calls } = setup();
    tap(g, 1, 0.2, 0.5, 0);
    tap(g, 2, 0.2, 0.5, 100);
    expect(calls).toEqual(["double:left"]);
  });
  it("treats two taps far apart as two single taps", () => {
    const { g, calls } = setup();
    tap(g, 1, 0.1, 0.5, 0);
    tap(g, 2, 0.9, 0.5, 100);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual(["tap", "tap"]);
  });
  it("treats two slow taps as two single taps", () => {
    const { g, calls } = setup();
    tap(g, 1, 0.5, 0.5, 0);
    vi.advanceTimersByTime(DOUBLE_TAP_MS + 50);
    tap(g, 2, 0.5, 0.5, DOUBLE_TAP_MS + 100);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual(["tap", "tap"]);
  });
  it("ignores a long press", () => {
    const { g, calls } = setup();
    g.down(1, 0.5, 0.5, 0);
    g.up(1, 0.5, 0.5, 900);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual([]);
  });
});

describe("swipe", () => {
  it("swipe left (finger moves left) goes to next", () => {
    const { g, calls } = setup();
    g.down(1, 0.8, 0.5, 0);
    g.move(1, 0.5, 0.52);
    g.up(1, 0.4, 0.52, 150);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual(["swipe:next"]);
  });
  it("swipe right goes to previous", () => {
    const { g, calls } = setup();
    g.down(1, 0.2, 0.5, 0);
    g.up(1, 0.6, 0.5, 200);
    expect(calls).toEqual(["swipe:previous"]);
  });
  it("ignores mostly vertical drags", () => {
    const { g, calls } = setup();
    g.down(1, 0.5, 0.2, 0);
    g.up(1, 0.58, 0.9, 200);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual([]);
  });
  it("ignores a short horizontal wobble as a tap", () => {
    const { g, calls } = setup();
    g.down(1, 0.5, 0.5, 0);
    g.up(1, 0.51, 0.5, 80);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual(["tap"]);
  });
  it("ignores a slow drag", () => {
    const { g, calls } = setup();
    g.down(1, 0.8, 0.5, 0);
    g.up(1, 0.2, 0.5, 2000);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual([]);
  });
});

describe("two-finger spread", () => {
  it("emits start, update and end with normalized corners", () => {
    const { g, calls, regions } = setup();
    g.down(1, 0.45, 0.45, 0);
    g.down(2, 0.55, 0.55, 10);
    g.move(1, 0.3, 0.3);
    g.move(2, 0.7, 0.7);
    g.up(1, 0.3, 0.3, 200);
    g.up(2, 0.7, 0.7, 210);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual([]);
    expect(regions[0].phase).toBe("start");
    expect(regions.some((r) => r.phase === "update")).toBe(true);
    const end = regions[regions.length - 1];
    expect(end.phase).toBe("end");
    expect(end.rect.x).toBeCloseTo(0.3);
    expect(end.rect.y).toBeCloseTo(0.3);
    expect(end.rect.w).toBeCloseTo(0.4);
    expect(end.rect.h).toBeCloseTo(0.4);
    expect(end.accidental).toBe(false);
  });
  it("orders corners whichever finger is left/top", () => {
    const { g, regions } = setup();
    g.down(1, 0.7, 0.7, 0);
    g.down(2, 0.3, 0.3, 0);
    g.up(1, 0.7, 0.7, 100);
    g.up(2, 0.3, 0.3, 100);
    const end = regions[regions.length - 1];
    expect(end.rect.x).toBeCloseTo(0.3);
    expect(end.rect.w).toBeCloseTo(0.4);
  });
  it("flags an accidental touch (two fingers lift almost in place)", () => {
    const { g, regions } = setup();
    g.down(1, 0.5, 0.5, 0);
    g.down(2, 0.51, 0.51, 5);
    g.up(1, 0.5, 0.5, 80);
    g.up(2, 0.51, 0.51, 85);
    const end = regions[regions.length - 1];
    expect(end.accidental).toBe(true);
    expect(MIN_REGION).toBe(0.06);
  });
  it("clamps corners to the stage", () => {
    const { g, regions } = setup();
    g.down(1, -0.2, 0.5, 0);
    g.down(2, 1.4, 0.9, 0);
    g.up(1, -0.2, 0.5, 100);
    g.up(2, 1.4, 0.9, 100);
    const end = regions[regions.length - 1];
    expect(end.rect.x).toBe(0);
    expect(end.rect.x + end.rect.w).toBe(1);
  });
  it("does not toggle controls or navigate when nobody listens", () => {
    const { g, calls } = setup(false);
    g.down(1, 0.3, 0.3, 0);
    g.down(2, 0.7, 0.7, 0);
    g.move(2, 0.9, 0.9);
    g.up(1, 0.3, 0.3, 100);
    g.up(2, 0.9, 0.9, 100);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual([]);
  });
  it("cancels a pending single tap when a second finger lands", () => {
    const { g, calls } = setup();
    tap(g, 1, 0.5, 0.5, 0);
    g.down(2, 0.4, 0.4, 100);
    g.down(3, 0.6, 0.6, 100);
    vi.advanceTimersByTime(1000);
    expect(calls).toEqual([]);
  });
  it("ignores a third finger", () => {
    const { g, regions } = setup();
    g.down(1, 0.3, 0.3, 0);
    g.down(2, 0.7, 0.7, 0);
    g.down(3, 0.9, 0.9, 0);
    g.move(3, 0.95, 0.95);
    g.up(3, 0.95, 0.95, 30);
    g.up(1, 0.3, 0.3, 100);
    g.up(2, 0.7, 0.7, 100);
    expect(regions.filter((r) => r.phase === "end")).toHaveLength(1);
  });
  it("pointercancel ends the gesture as accidental", () => {
    const { g, regions } = setup();
    g.down(1, 0.3, 0.3, 0);
    g.down(2, 0.7, 0.7, 0);
    g.cancel(1);
    g.cancel(2);
    expect(regions[regions.length - 1]).toMatchObject({
      phase: "end",
      accidental: true,
    });
  });
});

describe("normalizeRect", () => {
  it("orders and clamps", () => {
    expect(normalizeRect({ x: 0.9, y: 0.2 }, { x: 0.1, y: 1.5 })).toEqual({
      x: 0.1,
      y: 0.2,
      w: 0.8,
      h: 0.8,
    });
  });
});
