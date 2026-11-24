import { useEffect, useEffectEvent } from "react";
import { trackHotkey } from "../../lib/analytics";
import type { ScreenId } from "../shell/navigation";
import { isTextTarget, targetElement } from "./guards";
import { matchesShortcut } from "./matcher";
import { shortcuts, type ShortcutId, type ShortcutScope } from "./registry";

export { useEscapeLayer } from "./escape";

type Handler = (event: KeyboardEvent) => void | false;

type Options = {
  scope: ShortcutScope;
  surface: ScreenId;
  handlers: Partial<Record<ShortcutId, Handler>>;
  singleKeyShortcuts?: boolean;
};

/** Add data-hotkey-player to the player root. A focused descendant activates player bindings. */
export function useHotkeys({
  scope,
  surface,
  handlers,
  singleKeyShortcuts = true,
}: Options) {
  const handleKey = useEffectEvent((event: KeyboardEvent) => {
    const target = targetElement(event.target) ?? document.activeElement;
    if (scope === "player" && !target?.closest("[data-hotkey-player]")) return;
    const modifiersOnly = scope === "player" && !singleKeyShortcuts;
    if (modifiersOnly && !event.shiftKey && !event.ctrlKey && !event.metaKey)
      return;
    for (const shortcut of shortcuts) {
      const handler = handlers[shortcut.id];
      if (
        shortcut.scope !== scope ||
        shortcut.desktop ||
        !handler ||
        (modifiersOnly &&
          !shortcut.bindings.some((binding) => binding.includes("+"))) ||
        !matchesShortcut(shortcut.id, event, undefined, target)
      )
        continue;

      const textFocused = isTextTarget(target) || isTextTarget();
      if (handler(event) === false) continue;
      event.preventDefault();
      event.stopPropagation();
      if (!textFocused) trackHotkey(shortcut.id, surface);
      return;
    }
  });
  useEffect(() => {
    window.addEventListener("keydown", handleKey);
    return () => window.removeEventListener("keydown", handleKey);
  }, []);
}
