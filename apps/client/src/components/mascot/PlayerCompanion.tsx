import { useEffect, useState } from "react";
import { Mascot } from "./Mascot";
import { deriveMascotState } from "./state";
import { useMascotBus } from "./events";
import { useMascotAppearance, useMascotPreference } from "./preferences";

const useOnline = () => {
  const [on, set] = useState(() => navigator.onLine);
  useEffect(() => {
    const u = () => set(true),
      d = () => set(false);
    window.addEventListener("online", u);
    window.addEventListener("offline", d);
    return () => {
      window.removeEventListener("online", u);
      window.removeEventListener("offline", d);
    };
  }, []);
  return on;
};

/**
 * 48 px corner companion for the framed lesson player chrome (outside the slide viewport).
 * Presentation only: it never touches playback, grading or the sandboxed webview.
 * In immersive mode it shows only while speaking a hint or celebrating.
 */
export function PlayerCompanion({
  immersive = false,
}: {
  immersive?: boolean;
}) {
  const pref = useMascotPreference();
  const appearance = useMascotAppearance();
  const online = useOnline();
  const { cue, talking } = useMascotBus();
  if (!pref.companionVisible) return null;
  const speaking = talking || cue === "celebrate";
  if (immersive && !speaking) return null;
  const state = deriveMascotState(
    { offline: !online, streaming: talking },
    cue,
  );
  return (
    <span className="m-companion" data-immersive={immersive || undefined}>
      <Mascot
        size={48}
        view="bust"
        state={state}
        appearance={appearance}
        offline={!online}
        labelled={state !== "idle"}
      />
    </span>
  );
}
