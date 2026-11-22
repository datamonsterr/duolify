import { describe, expect, it } from "vitest";
import {
  clampRegion,
  defaultRegion,
  keyboardAdjust,
  moveRegion,
  noSelection,
  resizeRegion,
  selectionReducer,
} from "./region";

const r = { x: 0.2, y: 0.2, w: 0.4, h: 0.3 };

describe("region maths", () => {
  it("keeps a moved region inside the stage", () => {
    expect(moveRegion(r, 2, 2)).toEqual({ x: 0.6, y: 0.7, w: 0.4, h: 0.3 });
    expect(moveRegion(r, -2, -2)).toEqual({ x: 0, y: 0, w: 0.4, h: 0.3 });
  });
  it("resizes by a corner and keeps the opposite corner fixed", () => {
    const out = resizeRegion(r, "nw", 0.1, 0.05);
    expect(out.x + out.w).toBeCloseTo(0.6);
    expect(out.y + out.h).toBeCloseTo(0.5);
    expect(out.x).toBeCloseTo(0.3);
  });
  it("never shrinks below 6% or grows past the stage", () => {
    const tiny = resizeRegion(r, "se", -5, -5);
    expect(tiny.w).toBeCloseTo(0.06);
    expect(tiny.h).toBeCloseTo(0.06);
    const big = resizeRegion(r, "se", 5, 5);
    expect(big.x + big.w).toBe(1);
    expect(big.y + big.h).toBe(1);
    expect(clampRegion({ x: 0.9, y: 0.9, w: 0.5, h: 0.5 })).toEqual({
      x: 0.5,
      y: 0.5,
      w: 0.5,
      h: 0.5,
    });
  });
  it("keyboard: arrows move, shift resizes, alt is coarse, other keys do nothing", () => {
    expect(keyboardAdjust(r, { key: "ArrowRight" })!.x).toBeCloseTo(0.21);
    expect(
      keyboardAdjust(r, { key: "ArrowRight", altKey: true })!.x,
    ).toBeCloseTo(0.25);
    expect(
      keyboardAdjust(r, { key: "ArrowDown", shiftKey: true })!.h,
    ).toBeCloseTo(0.31);
    expect(keyboardAdjust(r, { key: "a" })).toBeNull();
  });
});

describe("selection reducer", () => {
  it("a spread draws, then fixes the region", () => {
    let s = selectionReducer(noSelection, { type: "gesture_start", rect: r });
    expect(s.phase).toBe("drawing");
    s = selectionReducer(s, {
      type: "gesture_end",
      rect: r,
      accidental: false,
    });
    expect(s).toMatchObject({ phase: "selected", method: "two_finger" });
  });
  it("an accidental touch is discarded", () => {
    const s = selectionReducer(
      { phase: "drawing", rect: r },
      { type: "gesture_end", rect: { ...r, w: 0.02 }, accidental: true },
    );
    expect(s).toEqual(noSelection);
  });
  it("Select area places the default rectangle; adjusting keeps the method", () => {
    let s = selectionReducer(noSelection, { type: "select_area" });
    expect(s).toEqual({
      phase: "selected",
      rect: defaultRegion(),
      method: "select_area",
    });
    s = selectionReducer(s, { type: "adjust", rect: r });
    expect(s).toMatchObject({ method: "select_area", rect: r });
    s = selectionReducer(s, { type: "adjust", rect: r, method: "keyboard" });
    expect(s).toMatchObject({ method: "keyboard" });
    expect(selectionReducer(noSelection, { type: "adjust", rect: r })).toBe(
      noSelection,
    );
  });
});
