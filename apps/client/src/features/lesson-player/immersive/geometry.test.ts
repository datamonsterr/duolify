import { describe, expect, it } from "vitest";
import {
  STAGE_H,
  STAGE_W,
  clientToStage,
  layoutStage,
  stageTransform,
} from "./geometry";

describe("layoutStage", () => {
  it("scales uniformly and letterboxes a wide landscape screen", () => {
    const l = layoutStage(1600, 720, "landscape");
    expect(l.rotated).toBe(false);
    expect(l.scale).toBeCloseTo(1);
    expect(l.width).toBeCloseTo(1280);
    expect(l.height).toBeCloseTo(720);
  });
  it("letterboxes by the limiting side on a 19.5:9 phone", () => {
    const l = layoutStage(844, 390, "landscape");
    expect(l.scale).toBeCloseTo(390 / STAGE_H);
    expect(l.width).toBeCloseTo((390 / STAGE_H) * STAGE_W);
    expect(l.width).toBeLessThanOrEqual(844);
  });
  it("keeps the 16:9 ratio at every size", () => {
    for (const [w, h] of [
      [844, 390],
      [1280, 800],
      [915, 412],
      [2000, 500],
    ] as const) {
      const l = layoutStage(w, h, "landscape");
      expect(l.width / l.height).toBeCloseTo(16 / 9);
    }
  });
  it("rotates when the screen is portrait and orientation is not locked", () => {
    const l = layoutStage(390, 844, "landscape");
    expect(l.rotated).toBe(true);
    // the stage's long edge follows the screen's long edge
    expect(l.scale).toBeCloseTo(Math.min(844 / STAGE_W, 390 / STAGE_H));
  });
  it("does not rotate a portrait screen when rotation is disabled", () => {
    expect(layoutStage(390, 844, "none").rotated).toBe(false);
  });
  it("never rotates a landscape screen", () => {
    expect(layoutStage(844, 390, "landscape").rotated).toBe(false);
  });
  it("builds the CSS transform", () => {
    expect(stageTransform(layoutStage(1280, 720, "landscape"))).toBe(
      "rotate(0deg) scale(1)",
    );
    expect(stageTransform(layoutStage(390, 844, "landscape"))).toMatch(
      /^rotate\(90deg\) scale\(/,
    );
  });
});

describe("clientToStage", () => {
  it("maps the centre of the screen to the centre of the stage", () => {
    const l = layoutStage(844, 390, "landscape");
    const p = clientToStage(422, 195, 844, 390, l);
    expect(p.x).toBeCloseTo(0.5);
    expect(p.y).toBeCloseTo(0.5);
  });
  it("maps the stage corners when letterboxed", () => {
    const l = layoutStage(1600, 720, "landscape");
    const left = (1600 - l.width) / 2;
    const tl = clientToStage(left, 0, 1600, 720, l);
    expect(tl.x).toBeCloseTo(0);
    expect(tl.y).toBeCloseTo(0);
    const br = clientToStage(left + l.width, 720, 1600, 720, l);
    expect(br.x).toBeCloseTo(1);
    expect(br.y).toBeCloseTo(1);
  });
  it("is not clamped outside the stage unless asked", () => {
    const l = layoutStage(1600, 720, "landscape");
    expect(clientToStage(0, 0, 1600, 720, l).x).toBeLessThan(0);
    expect(clientToStage(0, 0, 1600, 720, l, true).x).toBe(0);
  });
  it("inverts the 90° rotation: screen-down is stage-right", () => {
    const l = layoutStage(390, 844, "landscape");
    expect(l.rotated).toBe(true);
    // centre stays centre
    const c = clientToStage(195, 422, 390, 844, l);
    expect(c.x).toBeCloseTo(0.5);
    expect(c.y).toBeCloseTo(0.5);
    // a point lower on the screen is further right on the stage
    const low = clientToStage(195, 600, 390, 844, l);
    expect(low.x).toBeGreaterThan(0.5);
    expect(low.y).toBeCloseTo(0.5);
    // a point further left on the screen is further down the stage
    const left = clientToStage(100, 422, 390, 844, l);
    expect(left.y).toBeGreaterThan(0.5);
    expect(left.x).toBeCloseTo(0.5);
  });
});
