import { useEffect, useEffectEvent } from "react";
import { trackHotkey } from "../../lib/analytics";
import type { ScreenId } from "../shell/navigation";
import {
  isComposing,
  isTextTarget,
  matchesUnmodifiedKey,
  targetElement,
  type ShortcutEvent,
} from "./guards";
import { dismiss, show_help } from "./shortcuts.json";

const priority = {
  immersive: 0,
  side_panel: 1,
  sheet: 2,
  menu: 3,
  coach_mark: 4,
  tooltip: 5,
};

type LayerKind = keyof typeof priority;
type Layer = {
  kind: LayerKind;
  close: () => void;
  surface?: ScreenId;
  element?: () => HTMLElement | null;
};

const layers = new Set<Layer>();

export function closeOnEscape(
  event: ShortcutEvent,
  close: () => void,
  surface?: ScreenId,
) {
  if (!matchesUnmodifiedKey(event, dismiss.bindings[0])) return false;

  event.preventDefault();
  event.stopPropagation();
  if (surface && !isTextTarget(event.target) && !isTextTarget())
    trackHotkey("dismiss", surface);
  close();
  return true;
}

const helpEvent = "duolify:shortcut-help";

export function openShortcutHelp() {
  window.dispatchEvent(new Event(helpEvent));
}

// Bind help with the shell so the first keypress can load its lazy dialog.
export function useHelpShortcut(
  open: boolean,
  show: () => void,
  surface: ScreenId,
) {
  const onKey = useEffectEvent((event: KeyboardEvent) => {
    if (
      open ||
      !matchesUnmodifiedKey(event, show_help.bindings[0], true) ||
      isTextTarget(event.target) ||
      isTextTarget()
    )
      return;
    event.preventDefault();
    event.stopPropagation();
    show();
    trackHotkey("show_help", surface);
  });
  const onOpen = useEffectEvent(() => {
    if (!isTextTarget()) show();
  });
  useEffect(() => {
    window.addEventListener("keydown", onKey);
    window.addEventListener(helpEvent, onOpen);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener(helpEvent, onOpen);
    };
  }, []);
}

function closeTopLayer(event: KeyboardEvent) {
  if (event.key !== dismiss.bindings[0]) return;
  const dialog = targetElement(event.target)?.closest(
    'dialog[open], [role="dialog"][aria-modal="true"]',
  );
  let top: Layer | undefined;
  for (const layer of layers) {
    const element = layer.element?.();
    if (layer.element && !element?.isConnected) continue;
    if (dialog && element && !dialog.contains(element)) continue;
    if (dialog && !element && priority[layer.kind] < priority.sheet) continue;
    if (!top || priority[layer.kind] >= priority[top.kind]) top = layer;
  }
  if (!top) return;
  if (event.repeat && !isComposing(event)) event.preventDefault();
  closeOnEscape(event, top.close, top.surface);
}

export function useEscapeLayer(
  kind: LayerKind,
  close: (() => void) | undefined,
  surface?: ScreenId,
  element?: () => HTMLElement | null,
) {
  const active = close !== undefined;
  const onClose = useEffectEvent(() => close?.());
  const getElement = useEffectEvent(() => element?.() ?? null);
  const hasElement = element !== undefined;
  useEffect(() => {
    if (!active) return;
    const layer = {
      kind,
      close: onClose,
      surface,
      element: hasElement ? getElement : undefined,
    };
    if (!layers.size) window.addEventListener("keydown", closeTopLayer, true);
    layers.add(layer);
    return () => {
      layers.delete(layer);
      if (!layers.size)
        window.removeEventListener("keydown", closeTopLayer, true);
    };
  }, [active, kind, surface, hasElement]);
}
