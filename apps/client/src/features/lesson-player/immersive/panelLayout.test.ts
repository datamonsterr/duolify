import { describe, expect, it } from "vitest";
import { canDock, panelWidth, stageShift } from "./panelLayout";

describe("answer panel sizing", () => {
  it("is 40% of the width within 320..480", () => {
    expect(panelWidth(844)).toBe(338);
    expect(panelWidth(1280)).toBe(480);
    expect(panelWidth(700)).toBe(320);
  });
  it("docks only where a stage still fits beside it", () => {
    expect(canDock(844)).toBe(true);
    expect(canDock(390)).toBe(false);
  });
});

describe("stage shift", () => {
  // 844 wide screen, stage 844 × 475 (full width), panel 338.
  const ui = 844;
  const stage = 844;
  const panel = panelWidth(ui);
  it("does not move the stage when the region is already clear of the panel", () => {
    expect(
      stageShift({ x: 0.05, y: 0.2, w: 0.3, h: 0.3 }, ui, stage, panel),
    ).toEqual({ shift: 0, overflow: 0 });
  });
  it("moves the stage left just enough to uncover the region", () => {
    const r = { x: 0.5, y: 0.2, w: 0.3, h: 0.3 };
    const { shift, overflow } = stageShift(r, ui, stage, panel);
    // region right edge 0.8 * 844 + margin, minus the 506 px that stay visible
    expect(shift).toBe(Math.round(0.8 * 844 + 16 - (844 - panel)));
    expect(overflow).toBe(0);
  });
  it("never pushes the region off the left edge and reports what is still covered", () => {
    const r = { x: 0.05, y: 0.2, w: 0.9, h: 0.3 };
    const { shift, overflow } = stageShift(r, ui, stage, panel);
    expect(shift).toBe(
      Math.round(0.05 * 844 - 16) < 0 ? 0 : Math.round(0.05 * 844 - 16),
    );
    expect(overflow).toBeGreaterThan(0);
  });
  it("uses the letterbox: a narrow stage centred on a wide screen shifts less", () => {
    const a = stageShift(
      { x: 0.7, y: 0, w: 0.25, h: 0.3 },
      1280,
      800,
      panelWidth(1280),
    );
    const b = stageShift(
      { x: 0.7, y: 0, w: 0.25, h: 0.3 },
      1280,
      1280,
      panelWidth(1280),
    );
    expect(a.shift).toBeLessThan(b.shift);
  });
});
