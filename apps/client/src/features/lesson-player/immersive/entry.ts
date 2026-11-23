import { useEffect } from "react";
import { rotationAction, type EntryState } from "./presentation";
import { useLatest } from "./useLatest";

type FsDoc = Document & {
  webkitFullscreenElement?: Element | null;
  webkitExitFullscreen?: () => Promise<void> | void;
};
type FsEl = HTMLElement & {
  webkitRequestFullscreen?: () => Promise<void> | void;
};

// Leaving can happen while full screen is still being entered; the late grant must not stick.
let fullscreenWanted = false;

/** Best effort. Must run inside the user's tap; a rejection leaves the CSS-only presentation. */
export async function requestStageFullscreen(el: HTMLElement | null) {
  if (!el) return false;
  fullscreenWanted = true;
  try {
    if (el.requestFullscreen) {
      await el.requestFullscreen({ navigationUI: "hide" });
      if (!fullscreenWanted) exitStageFullscreen();
      return true;
    }
    const w = el as FsEl;
    if (w.webkitRequestFullscreen) {
      await w.webkitRequestFullscreen();
      if (!fullscreenWanted) exitStageFullscreen();
      return true;
    }
  } catch {
    /* iPhone Safari, denied permission, no user activation, sandboxed webview. */
  }
  return false;
}

export function exitStageFullscreen() {
  fullscreenWanted = false;
  const d = document as FsDoc;
  try {
    if (d.fullscreenElement) void document.exitFullscreen();
    else if (d.webkitFullscreenElement) void d.webkitExitFullscreen?.();
  } catch {
    /* already out */
  }
}

/**
 * "Touch device" for rotation entry. Android WebViews (emulators, some devices) can report
 * `(pointer: fine)` with a touchscreen, so a finger-primary pointer is not the only signal:
 * a hover-less device and the native phone app itself count too. Desktop windows never do.
 */
export function isTouchDevice(coarse: boolean, hoverNone: boolean) {
  const target = import.meta.env.VITE_MOBILE_PLATFORM;
  return coarse || hoverNone || target === "android" || target === "ios";
}

/** Turning the device to landscape enters immersive mode; turning back leaves it if rotation entered. */
export function useEnterOnRotate(
  setting: boolean,
  immersive: EntryState,
  act: (a: "enter" | "exit") => void,
) {
  const state = useLatest({ setting, immersive, act });
  useEffect(() => {
    if (typeof matchMedia !== "function") return;
    const landscape = matchMedia("(orientation: landscape)");
    const coarse = matchMedia("(pointer: coarse)");
    const hoverNone = matchMedia("(hover: none)");
    const onChange = () => {
      const s = state.current;
      const a = rotationAction({
        setting: s.setting,
        coarse: isTouchDevice(coarse.matches, hoverNone.matches),
        landscape: landscape.matches,
        immersive: s.immersive,
      });
      if (a) s.act(a);
    };
    landscape.addEventListener("change", onChange);
    return () => landscape.removeEventListener("change", onChange);
  }, [state]);
}
