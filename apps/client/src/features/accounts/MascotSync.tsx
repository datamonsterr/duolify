import { useEffect, useSyncExternalStore } from "react";
import { profileApi } from "./learner-context";
import { session } from "./session";

/** Loads the Learner's guide preference once per sign-in so every placement shows it (applies via readSettings). */
export function MascotSync() {
  useSyncExternalStore(session.subscribe, session.snapshot);
  const owner = session.current()?.id;
  const signedIn = Boolean(owner && !session.expired());
  useEffect(() => {
    if (signedIn && navigator.onLine)
      void profileApi.readSettings().catch(() => {});
  }, [owner, signedIn]);
  return null;
}
