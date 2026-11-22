import { useEffect, useState } from "react";
import { session } from "../accounts/session";
import { acknowledge, loadProgress, type Progress } from "./api";
import { takeReplay } from "./handoff";
import { Welcome } from "./Welcome";
import { CoachMarks } from "./CoachMarks";
import "./onboarding.css";

export default function Onboarding() {
  const owner = session.current()?.id ?? "";
  const [progress, setProgress] = useState<Progress | null>(null);
  const [ready, setReady] = useState(false);
  const [replay, setReplay] = useState(() => takeReplay(owner));
  const [deferred, setDeferred] = useState(false);
  const [tour, setTour] = useState(false);
  useEffect(() => {
    if (!owner) return;
    let active = true;
    const load = () =>
      void loadProgress(owner).then((value) => {
        if (active) setProgress(value);
      });
    load();
    const showReplay = () => {
      takeReplay(owner);
      setReplay(true);
    };
    window.addEventListener("online", load);
    window.addEventListener("duolify:onboarding-replay", showReplay);
    const settled = () => {
      if (document.querySelector("#main-content h1")) setReady(true);
    };
    settled();
    const observer = new MutationObserver(settled);
    observer.observe(document.getElementById("main-content")!, {
      childList: true,
      subtree: true,
    });
    return () => {
      active = false;
      observer.disconnect();
      window.removeEventListener("online", load);
      window.removeEventListener("duolify:onboarding-replay", showReplay);
    };
  }, [owner]);
  if (!owner || !ready) return null;
  if (
    replay ||
    tour ||
    (!deferred &&
      progress?.welcome_state !== "pending" &&
      progress?.tour_state === "pending")
  ) {
    return (
      <CoachMarks
        source={replay ? "settings" : "first_run"}
        onFinish={(state) => {
          acknowledge(owner, "tour_state", state);
          setProgress((value) =>
            value
              ? { ...value, tour_state: state }
              : { welcome_state: "completed", tour_state: state },
          );
          setTour(false);
          setReplay(false);
        }}
      />
    );
  }
  if (progress?.welcome_state === "pending")
    return (
      <Welcome
        owner={owner}
        onFinish={(state, startTour) => {
          acknowledge(owner, "welcome_state", state);
          setProgress((value) => ({
            welcome_state: state,
            tour_state: value?.tour_state ?? "pending",
          }));
          setDeferred(!startTour);
          setTour(startTour);
        }}
      />
    );
  return null;
}
