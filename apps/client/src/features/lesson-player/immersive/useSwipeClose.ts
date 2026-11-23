import { useEffect, useRef, type RefObject } from "react";
import {
  SPRING_MS,
  closeSign,
  followOffset,
  isHorizontalSwipe,
  shouldClose,
} from "./swipeClose";

/**
 * Drag a docked panel toward its edge to close it. A drag may start on `grab` elements only
 * (the panel's leading edge and header), so the reply log keeps its own vertical scroll and
 * text selection. A second finger anywhere cancels the drag (region select keeps priority).
 */
export function useSwipeClose({
  panel,
  grabSelector,
  enabled,
  onClose,
  reducedMotion,
  rotated = false,
}: {
  panel: RefObject<HTMLElement | null>;
  grabSelector: string;
  enabled: boolean;
  onClose: () => void;
  reducedMotion: boolean;
  /** The stage is turned 90deg clockwise: stage x runs down the screen, so map the finger into stage space. */
  rotated?: boolean;
}) {
  const latest = useRef({ onClose, reducedMotion });
  useEffect(() => {
    latest.current = { onClose, reducedMotion };
  });
  useEffect(() => {
    const el = panel.current;
    if (!el || !enabled) return;
    const sign = closeSign(getComputedStyle(el).direction === "rtl");
    let drag: {
      id: number;
      x0: number;
      y0: number;
      t0: number;
      dx: number;
      locked: boolean;
    } | null = null;
    const fingers = new Set<number>();
    const rest = (animate: boolean) => {
      el.style.transition =
        animate && !latest.current.reducedMotion
          ? `transform ${SPRING_MS}ms ease-out`
          : "";
      el.style.transform = "";
    };
    const cancel = () => {
      if (!drag) return;
      drag = null;
      rest(true);
    };
    const down = (e: PointerEvent) => {
      fingers.add(e.pointerId);
      if (fingers.size > 1) return cancel();
      if (!(e.target as Element).closest(grabSelector)) return;
      drag = {
        id: e.pointerId,
        x0: e.clientX,
        y0: e.clientY,
        t0: e.timeStamp,
        dx: 0,
        locked: false,
      };
    };
    const move = (e: PointerEvent) => {
      if (!drag || e.pointerId !== drag.id) return;
      const sx = e.clientX - drag.x0;
      const sy = e.clientY - drag.y0;
      const dx = rotated ? sy : sx;
      const dy = rotated ? -sx : sy;
      if (!drag.locked) {
        if (isHorizontalSwipe(dx, dy)) {
          drag.locked = true;
          el.setPointerCapture?.(e.pointerId);
        } else if (Math.abs(dy) > 10) return void (drag = null);
        else return;
      }
      drag.dx = dx;
      if (!latest.current.reducedMotion) {
        el.style.transition = "";
        el.style.transform = `translateX(${followOffset(dx, sign)}px)`;
      }
    };
    const up = (e: PointerEvent) => {
      fingers.delete(e.pointerId);
      if (!drag || e.pointerId !== drag.id) return;
      const d = drag;
      drag = null;
      const dt = Math.max(1, e.timeStamp - d.t0);
      const close =
        d.locked &&
        shouldClose({
          dx: d.dx,
          sign,
          extent: rotated
            ? el.getBoundingClientRect().height
            : el.getBoundingClientRect().width,
          velocity: (d.dx * sign) / dt,
        });
      if (close) latest.current.onClose();
      else rest(true);
    };
    const gone = (e: PointerEvent) => {
      fingers.delete(e.pointerId);
      if (drag?.id === e.pointerId) cancel();
    };
    document.addEventListener("pointerdown", down, true);
    document.addEventListener("pointermove", move);
    document.addEventListener("pointerup", up);
    document.addEventListener("pointercancel", gone, true);
    return () => {
      document.removeEventListener("pointerdown", down, true);
      document.removeEventListener("pointermove", move);
      document.removeEventListener("pointerup", up);
      document.removeEventListener("pointercancel", gone, true);
      rest(false);
    };
  }, [panel, grabSelector, enabled, rotated]);
}
