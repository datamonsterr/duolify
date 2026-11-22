import { useEffect, useRef, type ReactNode } from "react";
import { clientToStage } from "./geometry";
import type { RegionRect } from "./gestures";
import {
  keyboardAdjust,
  moveRegion,
  resizeRegion,
  type Handle,
  type SelectionMethod,
} from "./region";
import { StageSpace } from "./StageSpace";
import { useLayout } from "./store";

export type RegionSelectorProps = {
  /** The rectangle to show, normalized to the 16:9 stage (0..1). */
  region: RegionRect;
  /** While a two-finger spread is still in progress: the rectangle follows the fingers, no handles. */
  drawing?: boolean;
  /** Drag, handle or keyboard change. Store it and pass it back as `region`. */
  onChange: (region: RegionRect, method: SelectionMethod) => void;
  /** Enter on the region. */
  onConfirm?: (region: RegionRect) => void;
  /** Escape on the region. */
  onCancel?: () => void;
  /** Buttons shown next to the region (ask, note, clear ...). */
  actions?: ReactNode;
  /** Accessible name of the region. */
  label?: string;
  /** Move keyboard focus to the region when it first appears ("Select area" by keyboard). */
  autoFocus?: boolean;
};

const HANDLES: { id: Handle; name: string }[] = [
  { id: "nw", name: "top left" },
  { id: "ne", name: "top right" },
  { id: "sw", name: "bottom left" },
  { id: "se", name: "bottom right" },
];

type Drag = {
  kind: "move" | Handle;
  start: { x: number; y: number };
  from: RegionRect;
};

/**
 * Reusable region overlay for the immersive player: dimmed surround, an outline with four
 * corner handles, drag to move, arrow keys to move and Shift+arrow to resize. Mount it
 * only while a region exists. It draws in stage space (`StageSpace`), so rotation,
 * letterboxing and scale are already handled and every rectangle it reports is
 * normalized 0..1. It owns no state: pass `region`, store what `onChange` reports.
 */
export function RegionSelector({
  region,
  drawing = false,
  onChange,
  onConfirm,
  onCancel,
  actions,
  label = "Selected area",
  autoFocus = false,
}: RegionSelectorProps) {
  const layout = useLayout();
  const box = useRef<HTMLDivElement>(null);
  const drag = useRef<Drag | null>(null);
  useEffect(() => {
    if (autoFocus) box.current?.focus({ preventScroll: true });
  }, [autoFocus]);

  const point = (e: React.PointerEvent) =>
    clientToStage(e.clientX, e.clientY, layout.vw, layout.vh, layout);
  const begin = (e: React.PointerEvent<HTMLElement>) => {
    e.stopPropagation();
    e.currentTarget.setPointerCapture?.(e.pointerId);
    drag.current = {
      kind: e.currentTarget.dataset.drag as Drag["kind"],
      start: point(e),
      from: region,
    };
  };
  const track = (e: React.PointerEvent) => {
    const d = drag.current;
    if (!d) return;
    const p = point(e);
    const dx = p.x - d.start.x;
    const dy = p.y - d.start.y;
    onChange(
      d.kind === "move"
        ? moveRegion(d.from, dx, dy)
        : resizeRegion(d.from, d.kind, dx, dy),
      "select_area",
    );
  };
  const end = () => {
    drag.current = null;
  };
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      onCancel?.();
    } else if (e.key === "Enter" && e.target === e.currentTarget) {
      e.preventDefault();
      onConfirm?.(region);
    } else {
      const next = keyboardAdjust(region, e);
      if (next) {
        e.preventDefault();
        onChange(next, "keyboard");
      }
    }
  };

  const below = region.y + region.h < 0.78;
  return (
    <StageSpace testId="region-selector">
      <div
        ref={box}
        className="imm-region"
        data-testid="region-box"
        data-drawing={drawing || undefined}
        role={drawing ? undefined : "group"}
        tabIndex={drawing ? undefined : 0}
        aria-label={
          drawing
            ? undefined
            : `${label}. Arrow keys move it, Shift and arrow keys resize it, Enter confirms, Escape cancels.`
        }
        aria-hidden={drawing || undefined}
        onKeyDown={drawing ? undefined : onKeyDown}
        data-drag="move"
        onPointerDown={drawing ? undefined : begin}
        onPointerMove={track}
        onPointerUp={end}
        onPointerCancel={end}
        style={{
          left: `${region.x * 100}%`,
          top: `${region.y * 100}%`,
          width: `${region.w * 100}%`,
          height: `${region.h * 100}%`,
        }}
      >
        {!drawing &&
          HANDLES.map((h) => (
            <button
              key={h.id}
              type="button"
              className={`imm-region-handle imm-handle-${h.id}`}
              data-testid={`region-handle-${h.id}`}
              aria-label={`Resize ${h.name} corner`}
              data-drag={h.id}
              onPointerDown={begin}
              onPointerMove={track}
              onPointerUp={end}
              onPointerCancel={end}
              onKeyDown={(e) => {
                const probe = keyboardAdjust(region, {
                  key: e.key,
                  shiftKey: true,
                  altKey: e.altKey,
                });
                if (!probe) return;
                e.preventDefault();
                e.stopPropagation();
                const step = e.altKey ? 0.05 : 0.01;
                const dx =
                  e.key === "ArrowLeft"
                    ? -step
                    : e.key === "ArrowRight"
                      ? step
                      : 0;
                const dy =
                  e.key === "ArrowUp"
                    ? -step
                    : e.key === "ArrowDown"
                      ? step
                      : 0;
                onChange(resizeRegion(region, h.id, dx, dy), "keyboard");
              }}
            />
          ))}
      </div>
      {!drawing && actions && (
        <div
          className="imm-region-actions"
          data-testid="region-actions"
          style={{
            left: `${region.x * 100}%`,
            ...(below
              ? { top: `${(region.y + region.h) * 100}%` }
              : { bottom: `${(1 - region.y) * 100}%` }),
          }}
        >
          {actions}
        </div>
      )}
    </StageSpace>
  );
}
