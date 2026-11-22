import { useSyncExternalStore } from "react";
import {
  controlsReducer,
  initialControls,
  type ControlsAction,
  type ControlsState,
} from "./presentation";
import { layoutStage, type StageLayout } from "./geometry";

// Module state outlives the per-Slide remount of the player, so the control
// layer, lock and stage layout survive moving to another Slide.
function createStore<T>(initial: T) {
  let value = initial;
  const listeners = new Set<() => void>();
  return {
    get: () => value,
    set(next: T) {
      if (Object.is(next, value)) return;
      value = next;
      listeners.forEach((l) => l());
    },
    subscribe(l: () => void) {
      listeners.add(l);
      return () => void listeners.delete(l);
    },
  };
}

export const controlsStore = createStore<ControlsState>(initialControls(true));
export const dispatchControls = (a: ControlsAction) =>
  controlsStore.set(controlsReducer(controlsStore.get(), a));
export const resetControls = (visible: boolean) =>
  controlsStore.set(initialControls(visible));
export const useControls = () =>
  useSyncExternalStore(controlsStore.subscribe, controlsStore.get);

export const layoutStore = createStore<
  StageLayout & { vw: number; vh: number }
>({ ...layoutStage(1280, 720, "none"), vw: 1280, vh: 720 });
export const useLayout = () =>
  useSyncExternalStore(layoutStore.subscribe, layoutStore.get);

/** Keyboard or assistive use keeps the controls up and restores focus after a Slide change. */
const focus = { keyboard: false, id: null as string | null };
export const focusMemory = {
  get keyboard() {
    return focus.keyboard;
  },
  get id() {
    return focus.id;
  },
  set(next: Partial<typeof focus>) {
    Object.assign(focus, next);
  },
};
