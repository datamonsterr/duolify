import { useEffect } from "react";
import { trackImmersive } from "../../../lib/analytics";
import { layoutStage } from "./geometry";
import type { EntryTrigger, ExitReason } from "./presentation";
import {
  controlsStore,
  dispatchControls,
  focusMemory,
  layoutStore,
  resetControls,
} from "./store";
import { useLatest } from "./useLatest";
import "./immersive.css";

type Props = {
  trigger: EntryTrigger;
  courseId: string;
  lessonId: string;
  viaKeyboard: boolean;
  getSlideIndex: () => number;
  getExitReason: () => ExitReason;
  onReady: () => void;
  onExit: (reason: ExitReason) => void;
};

/**
 * Platform side of the immersive presentation. It lives above the per-Slide
 * remount, so full screen, the history entry and the layout survive moving to
 * another Slide. It never touches the Slide iframe or the clock.
 */
export default function ImmersiveHost({
  trigger,
  courseId,
  lessonId,
  viaKeyboard,
  getSlideIndex,
  getExitReason,
  onReady,
  onExit,
}: Props) {
  const exit = useLatest(onExit);
  const index = useLatest(getSlideIndex);
  const exitReason = useLatest(getExitReason);

  useEffect(() => {
    const startedAt = performance.now();
    let pushed = false;
    let ignorePop = false;
    let sawFullscreen = !!document.fullscreenElement;
    const finish = (r: ExitReason) => exit.current(r);

    // 1. Layout: uniform scale, letterbox, and rotation only where the screen is
    //    portrait on a touch device (the universal fallback for platforms that
    //    cannot lock orientation).
    const coarse = matchMedia("(pointer: coarse)");
    const root = document.documentElement;
    const relayout = () => {
      const vv = window.visualViewport;
      const vw = Math.round(vv?.width ?? window.innerWidth);
      const vh = Math.round(vv?.height ?? window.innerHeight);
      const l = layoutStage(vw, vh, coarse.matches ? "landscape" : "none");
      layoutStore.set({ ...l, vw, vh });
      root.style.setProperty("--imm-scale", String(l.scale));
      root.style.setProperty("--imm-rot", l.rotated ? "90deg" : "0deg");
      root.dataset.immRotated = String(l.rotated);
    };
    relayout();
    window.addEventListener("resize", relayout);
    window.visualViewport?.addEventListener("resize", relayout);

    // Safe-area insets (notch, home indicator) only exist with viewport-fit=cover; the
    // rest of the app is laid out without it, so it applies while immersive only.
    const meta = document.querySelector<HTMLMetaElement>(
      'meta[name="viewport"]',
    );
    const viewportBefore = meta?.content;
    if (meta && viewportBefore && !viewportBefore.includes("viewport-fit"))
      meta.content = `${viewportBefore}, viewport-fit=cover`;

    // 2. Orientation lock where the platform allows it (needs full screen on web).
    const lockOrientation = () => {
      const o = screen.orientation as
        | (ScreenOrientation & { lock?: (o: string) => Promise<void> })
        | undefined;
      if (o?.lock && document.fullscreenElement)
        void o.lock("landscape").then(relayout, () => undefined);
    };
    lockOrientation();

    // 3. Leave through full screen exit (Escape or system gesture inside full
    //    screen), Escape outside it, and the system back gesture.
    const onFullscreen = () => {
      if (document.fullscreenElement) {
        sawFullscreen = true;
        lockOrientation();
      } else if (sawFullscreen) finish("escape");
    };
    document.addEventListener("fullscreenchange", onFullscreen);
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      if (controlsStore.get().list) dispatchControls({ type: "closeList" });
      else finish("escape");
    };
    document.addEventListener("keydown", onKey);
    history.pushState({ ...(history.state ?? {}), immersive: true }, "");
    pushed = true;
    const onPop = () => {
      if (ignorePop) return;
      pushed = false;
      finish("back");
    };
    window.addEventListener("popstate", onPop);

    resetControls(true);
    focusMemory.set({ keyboard: viaKeyboard, id: viaKeyboard ? "play" : null });
    trackImmersive({
      event: "immersive_mode_entered",
      course_id: courseId,
      lesson_id: lessonId,
      slide_index: index.current(),
      trigger,
    });
    onReady();

    return () => {
      window.removeEventListener("resize", relayout);
      window.visualViewport?.removeEventListener("resize", relayout);
      document.removeEventListener("fullscreenchange", onFullscreen);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("popstate", onPop);
      if (meta && viewportBefore !== undefined) meta.content = viewportBefore;
      root.style.removeProperty("--imm-scale");
      root.style.removeProperty("--imm-rot");
      delete root.dataset.immRotated;
      try {
        screen.orientation?.unlock?.();
      } catch {
        /* never locked */
      }
      if (pushed && history.state?.immersive) {
        ignorePop = true;
        history.back();
      }
      trackImmersive({
        event: "immersive_mode_exited",
        course_id: courseId,
        lesson_id: lessonId,
        slide_index: index.current(), // eslint-disable-line react-hooks/exhaustive-deps
        trigger: exitReason.current(), // eslint-disable-line react-hooks/exhaustive-deps
        seconds_in_mode: (performance.now() - startedAt) / 1000,
      });
    };
    // Mount once per entry: props are read through refs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return null;
}
