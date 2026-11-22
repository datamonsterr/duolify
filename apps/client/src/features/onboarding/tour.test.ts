import { describe, expect, it } from "vitest";
import { advance, placeTooltip, mergeState } from "./tour";
describe("tour state", () => {
  it("moves through real steps, finishes and skips", () => {
    expect(advance(0, "back", 5)).toBe(0);
    expect(advance(0, "next", 5)).toBe(1);
    expect(advance(3, "back", 5)).toBe(2);
    expect(advance(4, "next", 5)).toBe("completed");
    expect(advance(0, "skip", 5)).toBe("skipped");
    expect(advance(0, "next", 0)).toBe("completed");
  });
  it("merges offline/concurrent states without downgrading", () => {
    expect(mergeState("pending", "skipped")).toBe("skipped");
    expect(mergeState("skipped", "completed")).toBe("completed");
    expect(mergeState("completed", "skipped")).toBe("completed");
  });
});
describe("tooltip placement", () => {
  it("places below a target, arrow at its centre", () => {
    const r = placeTooltip(
      { x: 200, y: 50, width: 80, height: 44 },
      { width: 320, height: 220 },
      { x: 0, y: 0, width: 1280, height: 900 },
    );
    expect(r.side).toBe("top");
    expect(r.y).toBeGreaterThan(94);
    expect(r.x + r.arrow).toBe(240);
  });
  it("flips above bottom tabs and clamps to mobile edges", () => {
    const r = placeTooltip(
      { x: 340, y: 800, width: 44, height: 60 },
      { width: 352, height: 250 },
      { x: 0, y: 0, width: 390, height: 900 },
    );
    expect(r.side).toBe("bottom");
    expect(r.x).toBeGreaterThanOrEqual(12);
    expect(r.x + 352).toBeLessThanOrEqual(378);
    expect(r.y + 250).toBeLessThan(800);
    expect(r.arrow).toBeLessThanOrEqual(328);
  });
  it("respects visual viewport offsets when a keyboard appears", () => {
    const r = placeTooltip(
      { x: 12, y: 180, width: 44, height: 44 },
      { width: 352, height: 250 },
      { x: 0, y: 100, width: 390, height: 400 },
    );
    expect(r.y).toBeGreaterThanOrEqual(112);
    expect(r.y + 250).toBeLessThanOrEqual(488);
  });
  it("places beside a tall target when there is horizontal space", () => {
    const r = placeTooltip(
      { x: 20, y: 300, width: 60, height: 400 },
      { width: 352, height: 300 },
      { x: 0, y: 0, width: 1280, height: 800 },
    );
    expect(r.side).toBe("left");
    expect(r.x).toBeGreaterThan(80);
  });
});
