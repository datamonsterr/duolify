import { useEffect, useState } from "react";
import type { MascotCue } from "./state";

/** Presentation-only bus: lets features cue the player companion without importing it. */
export const MASCOT_CUE_EVENT = "duolify:mascot-cue";
export const MASCOT_TALK_EVENT = "duolify:mascot-talk";
export const emitMascotCue = (cue: MascotCue) =>
  window.dispatchEvent(new CustomEvent(MASCOT_CUE_EVENT, { detail: cue }));
/** `on` true while a hint or message is spoken or shown. */
export const emitMascotTalk = (on: boolean) =>
  window.dispatchEvent(new CustomEvent(MASCOT_TALK_EVENT, { detail: on }));

export function useMascotBus(): { cue: MascotCue | null; talking: boolean } {
  const [cue, setCue] = useState<MascotCue | null>(null);
  const [talking, setTalking] = useState(false);
  useEffect(() => {
    let t: ReturnType<typeof setTimeout>;
    const onCue = (e: Event) => {
      setCue((e as CustomEvent<MascotCue>).detail);
      clearTimeout(t);
      t = setTimeout(() => setCue(null), 2600);
    };
    const onTalk = (e: Event) => setTalking((e as CustomEvent<boolean>).detail);
    window.addEventListener(MASCOT_CUE_EVENT, onCue);
    window.addEventListener(MASCOT_TALK_EVENT, onTalk);
    return () => {
      clearTimeout(t);
      window.removeEventListener(MASCOT_CUE_EVENT, onCue);
      window.removeEventListener(MASCOT_TALK_EVENT, onTalk);
    };
  }, []);
  return { cue, talking };
}
