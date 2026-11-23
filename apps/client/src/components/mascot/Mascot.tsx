import { Component, lazy, Suspense, type ReactNode } from "react";
import { trackMascot } from "../../lib/analytics";
import type { MascotProps } from "./MascotArt";

export type { MascotProps, MascotAppearance } from "./MascotArt";

// The art and its CSS load as a separate chunk so they stay out of the first-load budget.
const Art = lazy(() =>
  import("./MascotArt").then((m) => ({ default: m.MascotArt })),
);

const Character = lazy(
  () => import("../../features/characters/CharacterGuide"),
);

const Blank = ({ size }: { size: number }) => (
  <span
    aria-hidden="true"
    style={{ display: "inline-block", width: size, height: size }}
  />
);

/** A chunk that cannot load (offline, stale deploy) leaves a blank slot instead of breaking the screen. */
class ArtBoundary extends Component<
  { size: number; children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch() {
    trackMascot({ event: "mascot_state_fallback", reason: "art_failed" });
  }
  render() {
    return this.state.failed ? (
      <Blank size={this.props.size} />
    ) : (
      this.props.children
    );
  }
}

/** The mascot. Sizes from 32 px up; below 56 px it shows the bust for crispness. */
export function Mascot(props: MascotProps) {
  const size = props.size ?? 96;
  const builtIn = (
    <Suspense fallback={<Blank size={size} />}>
      <Art {...props} />
    </Suspense>
  );
  const id = props.appearance?.characterId;
  return (
    <ArtBoundary size={size}>
      {id ? (
        <Suspense fallback={<Blank size={size} />}>
          <Character
            characterId={id}
            state={props.state ?? "idle"}
            size={size}
            label={props.label}
            fallback={builtIn}
          />
        </Suspense>
      ) : (
        builtIn
      )}
    </ArtBoundary>
  );
}
