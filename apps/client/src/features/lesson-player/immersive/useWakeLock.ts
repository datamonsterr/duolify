import { useEffect } from "react";

type Sentinel = { release: () => Promise<void> };
type WakeLockApi = { request: (type: "screen") => Promise<Sentinel> };

/** Holds a screen wake lock while `active`; released on pause, background and exit. */
export function useWakeLock(active: boolean) {
  useEffect(() => {
    const api = (navigator as Navigator & { wakeLock?: WakeLockApi }).wakeLock;
    if (!active || !api) return;
    let sentinel: Sentinel | undefined;
    let cancelled = false;
    const acquire = async () => {
      try {
        const s = await api.request("screen");
        if (cancelled) void s.release().catch(() => undefined);
        else sentinel = s;
      } catch {
        /* Denied or unsupported: the screen may sleep, playback is unaffected. */
      }
    };
    const onVisible = () => {
      if (document.visibilityState === "hidden") {
        void sentinel?.release().catch(() => undefined);
        sentinel = undefined;
      } else if (!sentinel) void acquire();
    };
    void acquire();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      document.removeEventListener("visibilitychange", onVisible);
      void sentinel?.release().catch(() => undefined);
    };
  }, [active]);
}
