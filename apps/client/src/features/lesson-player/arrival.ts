import type { RegionRect } from "./immersive/gestures";

/** Where "Show in Lesson" lands: a time on the opened Slide and, optionally, a region to outline. */
export type NoteArrival = { timeMs: number; rect?: RegionRect };

/** Read `?t=<ms>&region=x,y,w,h`; anything malformed is ignored. */
export function parseArrival(
  t?: string,
  region?: string,
): NoteArrival | undefined {
  const timeMs = t !== undefined && /^\d{1,9}$/.test(t) ? Number(t) : undefined;
  let rect: RegionRect | undefined;
  const parts = region?.split(",").map(Number);
  if (parts?.length === 4 && parts.every((n) => Number.isFinite(n))) {
    const [x, y, w, h] = parts;
    if (
      x >= 0 &&
      y >= 0 &&
      w > 0 &&
      h > 0 &&
      x + w <= 1.0001 &&
      y + h <= 1.0001
    )
      rect = { x, y, w, h };
  }
  if (timeMs === undefined && !rect) return undefined;
  return { timeMs: timeMs ?? 0, rect };
}
