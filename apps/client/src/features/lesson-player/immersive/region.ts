// Pure maths for the region the Learner adjusts after a spread, "Select area" or the
// keyboard. Everything is in normalized stage coordinates (0..1 of the 16:9 stage).
import { MIN_REGION, type RegionRect } from "./gestures";

export type SelectionMethod = "two_finger" | "select_area" | "keyboard";
export type Handle = "nw" | "ne" | "sw" | "se";

/** Keyboard steps: 1% of the stage per arrow press, 5% with Alt. */
export const KEY_STEP = 0.01;
export const KEY_STEP_COARSE = 0.05;

const clamp = (n: number, lo: number, hi: number) =>
  Math.min(hi, Math.max(lo, n));

/** Where "Select area" places the rectangle: a centred 40 × 40% box to drag and resize. */
export const defaultRegion = (): RegionRect => ({
  x: 0.3,
  y: 0.3,
  w: 0.4,
  h: 0.4,
});

/** Keep a rectangle inside the stage and at least MIN_REGION in both directions. */
export function clampRegion(r: RegionRect): RegionRect {
  const w = clamp(r.w, MIN_REGION, 1);
  const h = clamp(r.h, MIN_REGION, 1);
  return {
    x: clamp(r.x, 0, 1 - w),
    y: clamp(r.y, 0, 1 - h),
    w,
    h,
  };
}

export function moveRegion(r: RegionRect, dx: number, dy: number): RegionRect {
  return clampRegion({ ...r, x: r.x + dx, y: r.y + dy });
}

/** Drag one corner by (dx, dy); the opposite corner stays put. */
export function resizeRegion(
  r: RegionRect,
  handle: Handle,
  dx: number,
  dy: number,
): RegionRect {
  let x0 = r.x;
  let y0 = r.y;
  let x1 = r.x + r.w;
  let y1 = r.y + r.h;
  if (handle === "nw" || handle === "sw")
    x0 = clamp(x0 + dx, 0, x1 - MIN_REGION);
  else x1 = clamp(x1 + dx, x0 + MIN_REGION, 1);
  if (handle === "nw" || handle === "ne")
    y0 = clamp(y0 + dy, 0, y1 - MIN_REGION);
  else y1 = clamp(y1 + dy, y0 + MIN_REGION, 1);
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
}

/**
 * Arrow keys move, Shift+arrow resizes (the south-east corner), Alt makes the step
 * coarse. Returns null for keys that do nothing here.
 */
export function keyboardAdjust(
  r: RegionRect,
  e: { key: string; shiftKey?: boolean; altKey?: boolean },
): RegionRect | null {
  const step = e.altKey ? KEY_STEP_COARSE : KEY_STEP;
  const dir = {
    ArrowLeft: [-step, 0],
    ArrowRight: [step, 0],
    ArrowUp: [0, -step],
    ArrowDown: [0, step],
  }[e.key];
  if (!dir) return null;
  return e.shiftKey
    ? resizeRegion(r, "se", dir[0], dir[1])
    : moveRegion(r, dir[0], dir[1]);
}

export type Selection =
  | { phase: "none" }
  | { phase: "drawing"; rect: RegionRect }
  | { phase: "selected"; rect: RegionRect; method: SelectionMethod };

export type SelectionAction =
  | { type: "gesture_start" | "gesture_update"; rect: RegionRect }
  | { type: "gesture_end"; rect: RegionRect; accidental: boolean }
  | { type: "select_area" }
  | { type: "adjust"; rect: RegionRect; method?: SelectionMethod }
  | { type: "clear" };

export const noSelection: Selection = { phase: "none" };

export function selectionReducer(
  state: Selection,
  a: SelectionAction,
): Selection {
  switch (a.type) {
    case "gesture_start":
    case "gesture_update":
      return { phase: "drawing", rect: a.rect };
    case "gesture_end":
      // A region under 6% of the stage in either direction is an accidental touch.
      return a.accidental
        ? noSelection
        : {
            phase: "selected",
            rect: clampRegion(a.rect),
            method: "two_finger",
          };
    case "select_area":
      return {
        phase: "selected",
        rect: defaultRegion(),
        method: "select_area",
      };
    case "adjust":
      if (state.phase === "none") return state;
      return {
        phase: "selected",
        rect: clampRegion(a.rect),
        method:
          a.method ??
          (state.phase === "selected" ? state.method : "select_area"),
      };
    case "clear":
      return noSelection;
  }
}
