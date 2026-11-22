// State of the immersive presentation. The player's own state (clock, position,
// narration) is not here: immersive mode is a second presentation of it.
export type EntryTrigger = "control" | "rotation";
export type ExitReason = "control" | "rotation" | "back" | "escape";
export type EntryState = { trigger: EntryTrigger } | null;
export type EntryEvent =
  | { type: "enter"; trigger: EntryTrigger }
  | { type: "exit"; reason: ExitReason };

export function entryReducer(state: EntryState, e: EntryEvent): EntryState {
  if (e.type === "enter") return state ?? { trigger: e.trigger };
  if (!state) return state;
  if (e.reason === "rotation" && state.trigger !== "rotation") return state;
  return null;
}

/** What turning the device means. `coarse` = the primary pointer is a finger. */
export function rotationAction(i: {
  setting: boolean;
  coarse: boolean;
  landscape: boolean;
  immersive: EntryState;
}): "enter" | "exit" | null {
  if (i.immersive) {
    return !i.landscape && i.immersive.trigger === "rotation" ? "exit" : null;
  }
  return i.setting && i.coarse && i.landscape ? "enter" : null;
}

export type ControlsState = {
  visible: boolean;
  locked: boolean;
  list: boolean;
};
export type ControlsAction = {
  type:
    "toggle" | "show" | "hide" | "lock" | "unlock" | "openList" | "closeList";
};

export const initialControls = (visible: boolean): ControlsState => ({
  visible,
  locked: false,
  list: false,
});

export function controlsReducer(
  s: ControlsState,
  a: ControlsAction,
): ControlsState {
  if (s.locked && a.type !== "unlock") return s;
  switch (a.type) {
    case "toggle":
      return s.visible
        ? { ...s, visible: false, list: false }
        : { ...s, visible: true };
    case "show":
      return s.visible ? s : { ...s, visible: true };
    case "hide":
      return s.visible ? { ...s, visible: false, list: false } : s;
    case "lock":
      return { visible: false, locked: true, list: false };
    case "unlock":
      return { visible: true, locked: false, list: false };
    case "openList":
      return { ...s, visible: true, list: true };
    case "closeList":
      return { ...s, list: false };
  }
}
