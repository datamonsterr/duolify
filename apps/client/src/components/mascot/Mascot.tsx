import { lazy, Suspense } from "react";
import type { MascotProps } from "./MascotArt";

export type { MascotProps, MascotAppearance } from "./MascotArt";

// The art and its CSS load as a separate chunk so they stay out of the first-load budget.
const Art = lazy(() =>
  import("./MascotArt").then((m) => ({ default: m.MascotArt })),
);

/** The mascot. Sizes from 32 px up; below 56 px it shows the bust for crispness. */
export function Mascot(props: MascotProps) {
  const size = props.size ?? 96;
  return (
    <Suspense
      fallback={
        <span
          aria-hidden="true"
          style={{ display: "inline-block", width: size, height: size }}
        />
      }
    >
      <Art {...props} />
    </Suspense>
  );
}
