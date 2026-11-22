import { useReducer } from "react";
import type { ImmersiveExtensions } from "./extensions";
import type { RegionRect } from "./gestures";
import {
  noSelection,
  selectionReducer,
  type Selection,
  type SelectionMethod,
} from "./region";

export type RegionSelectionOptions = {
  /** The Learner started a selection (two-finger spread or "Select area"): pause here. */
  onStart?: () => void;
  /** A spread turned out to be an accidental touch and was thrown away: resume here. */
  onDiscard?: () => void;
};

/**
 * State and gesture wiring for one region selection. Spread it into
 * `ImmersiveExtensionsContext.Provider` and render `<RegionSelector>` from
 * `selection`. See `docs/design/region-selection.md`.
 */
export function useRegionSelection({
  onStart,
  onDiscard,
}: RegionSelectionOptions = {}) {
  const [selection, dispatch] = useReducer(selectionReducer, noSelection);
  const gestureExtensions: Pick<
    ImmersiveExtensions,
    "onRegionGestureStart" | "onRegionGestureUpdate" | "onRegionGestureEnd"
  > = {
    onRegionGestureStart: (rect) => {
      onStart?.();
      dispatch({ type: "gesture_start", rect });
    },
    onRegionGestureUpdate: (rect) => dispatch({ type: "gesture_update", rect }),
    onRegionGestureEnd: (rect, { accidental }) => {
      dispatch({ type: "gesture_end", rect, accidental });
      if (accidental) onDiscard?.();
    },
  };
  const selectArea = () => {
    onStart?.();
    dispatch({ type: "select_area" });
  };
  const adjust = (rect: RegionRect, method?: SelectionMethod) =>
    dispatch({ type: "adjust", rect, method });
  const clear = () => dispatch({ type: "clear" });
  return { selection, gestureExtensions, selectArea, adjust, clear };
}

export type RegionSelectionState = Selection;
