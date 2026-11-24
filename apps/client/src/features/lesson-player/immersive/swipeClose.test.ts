import { describe, expect, it } from "vitest";
import {
  AXIS_RATIO,
  closeSign,
  followOffset,
  isHorizontalSwipe,
  shouldClose,
} from "./swipeClose";

describe("swipe close maths", () => {
  it("mirrors the closing direction in RTL", () => {
    expect(closeSign(false)).toBe(1);
    expect(closeSign(true)).toBe(-1);
  });
  it("follows one to one toward the edge and resists away from it", () => {
    expect(followOffset(100, 1)).toBe(100);
    expect(followOffset(-100, 1)).toBeCloseTo(-30);
    expect(followOffset(-100, -1)).toBe(-100);
    expect(followOffset(100, -1)).toBeCloseTo(30);
  });
  it("closes past 40% of the width", () => {
    const base = { sign: 1 as const, extent: 360, velocity: 0 };
    expect(shouldClose({ ...base, dx: 143 })).toBe(false);
    expect(shouldClose({ ...base, dx: 145 })).toBe(true);
  });
  it("closes on a flick after 24 px and never away from the edge", () => {
    const base = { sign: 1 as const, extent: 360 };
    expect(shouldClose({ ...base, dx: 30, velocity: 0.8 })).toBe(true);
    expect(shouldClose({ ...base, dx: 20, velocity: 0.8 })).toBe(false);
    expect(shouldClose({ ...base, dx: -300, velocity: -0.9 })).toBe(false);
  });
  it("RTL closes toward the left", () => {
    expect(shouldClose({ dx: -200, sign: -1, extent: 360, velocity: 0 })).toBe(
      true,
    );
    expect(shouldClose({ dx: 200, sign: -1, extent: 360, velocity: 0 })).toBe(
      false,
    );
  });
  it("locks to horizontal only when clearly horizontal", () => {
    expect(isHorizontalSwipe(40, 10)).toBe(true);
    expect(isHorizontalSwipe(40, 40 / AXIS_RATIO + 1)).toBe(false);
    expect(isHorizontalSwipe(5, 0)).toBe(false);
  });
});
