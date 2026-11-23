import type { RegionAsk } from "./tutor";

/** A miniature 16:9 stage with the asked-about region outlined. */
export function RegionThumb({ rect }: { rect: RegionAsk["rect"] }) {
  return (
    <div
      className="region-thumb"
      data-testid="region-thumb"
      role="img"
      aria-label="The area of the Slide your question is about"
    >
      <span
        style={{
          left: `${rect.x * 100}%`,
          top: `${rect.y * 100}%`,
          width: `${rect.w * 100}%`,
          height: `${rect.h * 100}%`,
        }}
      />
    </div>
  );
}
