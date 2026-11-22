import type { RegionRect } from "./gestures";

/**
 * A static outline around a region, in percent of the box it is placed in: the
 * framed `.stage-frame` or a `StageSpace`. It takes no pointer events.
 */
export function RegionOutline({
  region,
  label = "Selected area",
  testId = "region-outline",
}: {
  region: RegionRect;
  label?: string;
  testId?: string;
}) {
  return (
    <div
      className="region-outline"
      data-testid={testId}
      role="img"
      aria-label={label}
      style={{
        left: `${region.x * 100}%`,
        top: `${region.y * 100}%`,
        width: `${region.w * 100}%`,
        height: `${region.h * 100}%`,
      }}
    />
  );
}
