import { useCallback, useEffect, useReducer, useRef } from "react";
import type { MascotState } from "./rig-part-spec";

/** UI signals; the state is derived from these, never from model output. */
export type MascotSignals = {
  offline?: boolean;
  error?: boolean;
  waiting?: boolean;
  streaming?: boolean;
  /** Composer or widget focused, or the Learner is typing or recording. */
  listening?: boolean;
};
export type MascotCue = "happy" | "celebrate";

export const CUE_MS: Record<MascotCue, number> = {
  happy: 1500,
  celebrate: 2600,
};
export const SLEEPY_AFTER_MS = 60_000;

/** Pure priority order: offline, celebrate, confused, thinking, talking, listening, happy, sleepy, idle. */
export function deriveMascotState(
  s: MascotSignals,
  cue: MascotCue | null = null,
  asleep = false,
): MascotState {
  if (s.offline) return "sleepy";
  if (cue === "celebrate") return "celebrate";
  if (s.error) return "confused";
  if (s.waiting) return "thinking";
  if (s.streaming) return "talking";
  if (s.listening) return "listening";
  if (cue === "happy") return "happy";
  return asleep ? "sleepy" : "idle";
}

type Local = { cue: MascotCue | null; asleep: boolean };
type Act =
  | { t: "cue"; cue: MascotCue }
  | { t: "clear" }
  | { t: "sleep" }
  | { t: "wake" };
const reduce = (s: Local, a: Act): Local =>
  a.t === "cue"
    ? { cue: a.cue, asleep: false }
    : a.t === "clear"
      ? { ...s, cue: null }
      : a.t === "sleep"
        ? { ...s, asleep: true }
        : { ...s, asleep: false };

/**
 * Binds UI signals to the eight mascot states. `cue("celebrate")` and `cue("happy")` are
 * transient and return to the derived state; with no change for `sleepyAfterMs` it goes sleepy.
 * Never blocks input: it only returns a label for presentation.
 */
export function useMascotState(
  signals: MascotSignals = {},
  sleepyAfterMs = SLEEPY_AFTER_MS,
) {
  const [local, dispatch] = useReducer(reduce, { cue: null, asleep: false });
  const key = [
    signals.offline,
    signals.error,
    signals.waiting,
    signals.streaming,
    signals.listening,
  ]
    .map(Boolean)
    .join();
  useEffect(() => {
    dispatch({ t: "wake" });
    const id = setTimeout(() => dispatch({ t: "sleep" }), sleepyAfterMs);
    return () => clearTimeout(id);
  }, [key, local.cue, sleepyAfterMs]);
  useEffect(() => {
    if (!local.cue) return;
    const id = setTimeout(() => dispatch({ t: "clear" }), CUE_MS[local.cue]);
    return () => clearTimeout(id);
  }, [local.cue]);
  const cue = useCallback((c: MascotCue) => dispatch({ t: "cue", cue: c }), []);
  return { state: deriveMascotState(signals, local.cue, local.asleep), cue };
}

/** True for `ms` after each ping; for "the Learner is typing" without storing text. */
export function useActivity(ms = 1500): [boolean, () => void] {
  const [on, set] = useReducer((_: boolean, v: boolean) => v, false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const ping = useCallback(() => {
    set(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => set(false), ms);
  }, [ms]);
  useEffect(() => () => clearTimeout(timer.current), []);
  return [on, ping];
}
