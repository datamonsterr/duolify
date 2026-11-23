import { createContext, useContext, type ReactNode } from "react";
import type { RegionGestureEndInfo, RegionRect } from "./gestures";

/**
 * What later tasks mount into the immersive presentation. Provide it with
 * `<ImmersiveExtensionsContext.Provider value={...}>` around the lesson player.
 * With nothing provided the side rail is empty and the two-finger spread does
 * nothing visible.
 */
export type ImmersiveExtensions = {
  /** Rendered inside the side rail slot (`data-testid="immersive-side-rail-slot"`). */
  sideRail?: ReactNode;
  /** A note, question or selection is open: nothing may move the Slide on its own. */
  busy?: boolean;
  /** A tap on the stage; return true when it was used (it then does not toggle the controls). */
  onStageTap?: () => boolean;
  /** Two-finger spread. Rectangles are normalized to the 16:9 stage (0..1). */
  onRegionGestureStart?: (rect: RegionRect) => void;
  onRegionGestureUpdate?: (rect: RegionRect) => void;
  onRegionGestureEnd?: (rect: RegionRect, info: RegionGestureEndInfo) => void;
};

export const ImmersiveExtensionsContext = createContext<ImmersiveExtensions>(
  {},
);
export const useImmersiveExtensions = () =>
  useContext(ImmersiveExtensionsContext);
