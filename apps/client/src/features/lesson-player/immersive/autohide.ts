export const AUTO_HIDE_MS = 3000;

export type AutoHideBlockers = {
  /** Keyboard or switch focus is inside the control layer. */
  focusInside: boolean;
  /** A screen reader or other assistive technology is (heuristically) in use. */
  assistive: boolean;
  /** Playback stopped at the end of a Slide: next / replay must stay offered. */
  stoppedAtEnd: boolean;
  locked: boolean;
  /** Something modal or transient is open (Slide list, countdown). */
  pinned: boolean;
};

export const canAutoHide = (b: AutoHideBlockers) =>
  !b.focusInside && !b.assistive && !b.stoppedAtEnd && !b.pinned;

export function createAutoHide(onHide: () => void, delay = AUTO_HIDE_MS) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const clear = () => {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
  };
  return {
    /** (Re)start the countdown, or stop it when hiding is not allowed. */
    arm(allowed: boolean) {
      clear();
      if (allowed) timer = setTimeout(onHide, delay);
    },
    dispose: clear,
  };
}
