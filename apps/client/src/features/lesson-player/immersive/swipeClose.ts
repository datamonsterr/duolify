// Swipe-to-close for panels docked to one screen edge. Pure maths; `useSwipeClose` wires it to pointer events.
/** Travel needed, as a fraction of the panel's extent, to close it. */
export const CLOSE_FRACTION = 0.4;
/** A flick this fast (px/ms) closes after FLICK_MIN_PX of travel. */
export const FLICK_VELOCITY = 0.5;
export const FLICK_MIN_PX = 24;
/** The panel moves this much per px when dragged away from its edge. */
export const RESISTANCE = 0.3;
/** Movement before the drag picks an axis. */
export const LOCK_PX = 10;
/** Horizontal must beat vertical by this ratio to count as a swipe. */
export const AXIS_RATIO = 1.5;
export const SPRING_MS = 200;

/** Which way is "toward the docked edge": +1 is screen right. A panel at the inline end of an RTL page sits left. */
export const closeSign = (rtl: boolean): 1 | -1 => (rtl ? -1 : 1);

/** Panel offset in px for a finger travel `dx`: one to one toward the edge, damped away from it. */
export function followOffset(dx: number, sign: 1 | -1): number {
  const toward = dx * sign;
  return (toward >= 0 ? toward : toward * RESISTANCE) * sign;
}

export function isHorizontalSwipe(dx: number, dy: number): boolean {
  return (
    Math.max(Math.abs(dx), Math.abs(dy)) >= LOCK_PX &&
    Math.abs(dx) >= Math.abs(dy) * AXIS_RATIO
  );
}

export function shouldClose(p: {
  dx: number;
  sign: 1 | -1;
  extent: number;
  /** px per ms toward the edge (negative: away from it) */
  velocity: number;
}): boolean {
  const toward = p.dx * p.sign;
  if (toward <= 0) return false;
  return (
    toward >= p.extent * CLOSE_FRACTION ||
    (p.velocity >= FLICK_VELOCITY && toward >= FLICK_MIN_PX)
  );
}
