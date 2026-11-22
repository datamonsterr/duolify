// Layout maths for the answer panel docked to the right edge of the immersive stage.
// All lengths are CSS px along the immersive UI's own horizontal axis: the screen's x axis,
// or its y axis where the stage is rotated (the UI is rotated with it).
import type { RegionRect } from "./gestures";

export const PANEL_MIN = 320;
export const PANEL_MAX = 480;
/** Narrower than this there is no room to dock a panel beside a stage: use the tutor sheet. */
export const DOCK_MIN_WIDTH = 600;
const REGION_MARGIN = 16;

/** About 40% of the width, between 320 and 480 px. */
export const panelWidth = (uiWidth: number) =>
  Math.round(Math.min(PANEL_MAX, Math.max(PANEL_MIN, uiWidth * 0.4)));

export const canDock = (uiWidth: number) => uiWidth >= DOCK_MIN_WIDTH;

/**
 * How far to move the stage left so the whole region stays left of the panel. The stage
 * never moves further than its own left edge allows; when the region still does not fit, the
 * remainder is reported as `overflow` (the region is partly under the panel).
 */
export function stageShift(
  region: RegionRect,
  uiWidth: number,
  stageWidth: number,
  panel: number,
): { shift: number; overflow: number } {
  const left = (uiWidth - stageWidth) / 2;
  const regionRight = left + (region.x + region.w) * stageWidth + REGION_MARGIN;
  const regionLeft = left + region.x * stageWidth - REGION_MARGIN;
  const need = Math.max(0, regionRight - (uiWidth - panel));
  const room = Math.max(0, regionLeft);
  const shift = Math.min(need, room);
  return { shift: Math.round(shift), overflow: Math.round(need - shift) };
}
