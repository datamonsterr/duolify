import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import type { ScreenId } from "../shell/navigation";
import { useEscapeLayer } from "./escape";
import { shortcutMessage } from "./messages";
import {
  shortcutAria,
  shortcutById,
  shortcutKeys,
  shortcutPlatform,
  type ShortcutId,
} from "./registry";
import "./hotkeys.css";

export function ShortcutButton({
  id,
  label,
  surface,
  onClick,
}: {
  id: ShortcutId;
  label: ReactNode;
  surface: ScreenId;
  onClick: () => void;
}) {
  const { preferences } = useUiPreferences();
  const [show, setShow] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const owner = useRef<HTMLSpanElement>(null);
  const hint = useId();
  const keys = shortcutKeys(id, shortcutPlatform());
  const definition = shortcutById[id];
  function cancel() {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
  }
  function hide() {
    cancel();
    setShow(false);
  }
  function openAfter(delay: number) {
    cancel();
    timer.current = setTimeout(() => setShow(true), delay);
  }
  useEffect(() => cancel, []);
  useEscapeLayer(
    "tooltip",
    show ? hide : undefined,
    surface,
    () => owner.current,
  );
  return (
    <span className="shortcut-button" ref={owner}>
      <button
        className="duo-button quiet"
        type="button"
        aria-keyshortcuts={shortcutAria(id)}
        aria-describedby={show ? hint : undefined}
        onMouseEnter={() => openAfter(400)}
        onMouseLeave={hide}
        onFocus={() => openAfter(400)}
        onBlur={hide}
        onPointerDown={(event) => {
          if (event.pointerType === "touch") openAfter(500);
        }}
        onPointerUp={(event) => {
          if (event.pointerType === "touch") hide();
        }}
        onPointerCancel={hide}
        onClick={() => {
          hide();
          onClick();
        }}
      >
        {label}
      </button>
      {show && (
        <span id={hint} className="shortcut-tooltip" role="tooltip">
          {shortcutMessage(
            preferences.locale,
            definition.tooltip ?? definition.label,
          )}
          {keys[0] && <kbd dir="ltr">{keys[0]}</kbd>}
        </span>
      )}
    </span>
  );
}
