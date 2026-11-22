// Pointer gesture recogniser for the host-owned layer above the Slide iframe.
// Pure and framework-free: feed it pointer events in normalized stage coordinates
// (0..1, rotation already undone) and the displayed stage size in CSS px.
export const DOUBLE_TAP_MS = 300;
export const TAP_MAX_MS = 350;
export const TAP_MOVE_PX = 10;
export const DOUBLE_TAP_DIST_PX = 48;
export const SWIPE_MIN_PX = 56;
export const SWIPE_MAX_MS = 800;
export const SWIPE_AXIS_RATIO = 1.5;
/** A region narrower or shorter than this fraction of the stage is an accidental touch. */
export const MIN_REGION = 0.06;

export type RegionRect = { x: number; y: number; w: number; h: number };
export type RegionGestureEndInfo = { accidental: boolean };

/**
 * Internal contract for the two-finger spread. Coordinates are normalized to the
 * 16:9 stage, rotation and letterboxing already removed. With no handlers attached
 * the gesture is recognised and swallowed: nothing is toggled, paused or drawn.
 */
export type GestureHandlers = {
  onTap?: () => void;
  onDoubleTap?: (side: "left" | "right") => void;
  onSwipe?: (direction: "previous" | "next") => void;
  onRegionGestureStart?: (rect: RegionRect) => void;
  onRegionGestureUpdate?: (rect: RegionRect) => void;
  onRegionGestureEnd?: (rect: RegionRect, info: RegionGestureEndInfo) => void;
};

type Pt = { x: number; y: number };
const clamp01 = (n: number) => Math.min(1, Math.max(0, n));

export function normalizeRect(a: Pt, b: Pt): RegionRect {
  const x0 = clamp01(Math.min(a.x, b.x));
  const y0 = clamp01(Math.min(a.y, b.y));
  const x1 = clamp01(Math.max(a.x, b.x));
  const y1 = clamp01(Math.max(a.y, b.y));
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
}

export const isAccidentalRegion = (r: RegionRect) =>
  r.w < MIN_REGION || r.h < MIN_REGION;

export function createGestureRecognizer(
  handlers: GestureHandlers,
  getSize: () => { w: number; h: number },
) {
  const pointers = new Map<
    number,
    { x0: number; y0: number; t0: number; x: number; y: number }
  >();
  let region: {
    ids: [number, number];
    rect: RegionRect;
    frozen: Map<number, Pt>;
  } | null = null;
  let suppress = false; // set after a multi-touch gesture until every finger lifts
  let pending: { x: number; y: number; t: number; timer: number } | null = null;

  const flushPending = () => {
    if (!pending) return;
    clearTimeout(pending.timer);
    pending = null;
    handlers.onTap?.();
  };
  const dropPending = () => {
    if (!pending) return;
    clearTimeout(pending.timer);
    pending = null;
  };
  const px = (dx: number, dy: number) => {
    const s = getSize();
    return { dx: dx * s.w, dy: dy * s.h };
  };
  const pointOf = (id: number): Pt | undefined =>
    pointers.get(id) ?? region?.frozen.get(id);
  const rectOf = (ids: [number, number]) => {
    const a = pointOf(ids[0]);
    const b = pointOf(ids[1]);
    return a && b
      ? normalizeRect(a, b)
      : (region?.rect ?? { x: 0, y: 0, w: 0, h: 0 });
  };

  function down(id: number, x: number, y: number, t: number) {
    if (pointers.has(id)) return;
    pointers.set(id, { x0: x, y0: y, t0: t, x, y });
    if (region) return; // third finger: ignored
    if (pointers.size === 2) {
      dropPending();
      suppress = true;
      const ids = [...pointers.keys()].slice(0, 2) as [number, number];
      const rect = rectOf(ids);
      region = { ids, rect, frozen: new Map() };
      handlers.onRegionGestureStart?.(rect);
    }
  }

  function move(id: number, x: number, y: number) {
    const p = pointers.get(id);
    if (!p) return;
    p.x = x;
    p.y = y;
    if (region && region.ids.includes(id)) {
      region.rect = rectOf(region.ids);
      handlers.onRegionGestureUpdate?.(region.rect);
    }
  }

  function finishRegion(accidentalForced: boolean) {
    if (!region) return;
    const rect = region.rect;
    region = null;
    handlers.onRegionGestureEnd?.(rect, {
      accidental: accidentalForced || isAccidentalRegion(rect),
    });
  }

  function release(
    id: number,
    x: number,
    y: number,
    t: number,
    cancelled: boolean,
  ) {
    const p = pointers.get(id);
    if (!p) return;
    p.x = x;
    p.y = y;
    pointers.delete(id);
    if (region) {
      if (region.ids.includes(id)) {
        region.frozen.set(id, { x, y });
        region.rect = rectOf(region.ids);
        if (cancelled || region.frozen.size === 2) finishRegion(cancelled);
      }
      if (pointers.size === 0) suppress = false;
      return;
    }
    if (suppress) {
      if (pointers.size === 0) suppress = false;
      return;
    }
    if (cancelled) return;
    const { dx, dy } = px(x - p.x0, y - p.y0);
    const dur = t - p.t0;
    const dist = Math.hypot(dx, dy);
    if (dist < TAP_MOVE_PX) {
      if (dur > TAP_MAX_MS) return; // long press: reserved, does nothing
      if (
        pending &&
        t - pending.t <= DOUBLE_TAP_MS &&
        Math.hypot(
          ...(Object.values(px(x - pending.x, y - pending.y)) as [
            number,
            number,
          ]),
        ) <= DOUBLE_TAP_DIST_PX
      ) {
        dropPending();
        handlers.onDoubleTap?.(x < 0.5 ? "left" : "right");
        return;
      }
      flushPending();
      const timer = setTimeout(() => {
        pending = null;
        handlers.onTap?.();
      }, DOUBLE_TAP_MS) as unknown as number;
      pending = { x, y, t, timer };
      return;
    }
    if (
      Math.abs(dx) >= SWIPE_MIN_PX &&
      Math.abs(dx) >= SWIPE_AXIS_RATIO * Math.abs(dy) &&
      dur <= SWIPE_MAX_MS
    ) {
      flushPending();
      handlers.onSwipe?.(dx < 0 ? "next" : "previous");
    }
  }

  return {
    down,
    move,
    up: (id: number, x: number, y: number, t: number) =>
      release(id, x, y, t, false),
    cancel: (id: number) => {
      const p = pointers.get(id);
      release(id, p?.x ?? 0, p?.y ?? 0, 0, true);
    },
    dispose() {
      dropPending();
      pointers.clear();
      region = null;
      suppress = false;
    },
  };
}
