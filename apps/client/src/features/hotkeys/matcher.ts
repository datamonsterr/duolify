import {
  isComposing,
  isTextTarget,
  targetElement,
  type ShortcutEvent,
} from "./guards";
import {
  bindingParts,
  shortcutById,
  shortcutPlatform,
  type ShortcutId,
  type ShortcutPlatform,
} from "./registry";

export function matchesShortcut(
  id: ShortcutId,
  event: ShortcutEvent,
  platform: ShortcutPlatform = shortcutPlatform(),
  target = event.target,
  finePointer = window.matchMedia?.("(pointer: fine)").matches ?? true,
) {
  const shortcut = shortcutById[id];
  if (
    event.defaultPrevented ||
    isComposing(event) ||
    (event.repeat && !shortcut.repeat)
  )
    return false;
  if (event.altKey) return false;

  const element = targetElement(target);
  if (
    shortcut.scope === "player" &&
    element?.closest('input[type="range"], [role="slider"]')
  )
    return false;
  if (id !== "dismiss" && id !== "composer_send" && isTextTarget(target))
    return false;
  if (id === "composer_send" && !isTextTarget(target)) return false;

  return shortcut.bindings.some((binding) => {
    const { mod, key } = bindingParts(binding);
    const primary = mod === "Primary";
    if (
      event.ctrlKey !== (primary && platform === "other") ||
      event.metaKey !== (primary && platform === "mac")
    )
      return false;
    if (!/[?<>/–]/.test(key) && event.shiftKey !== (mod === "Shift"))
      return false;
    if (id === "composer_send" && !primary && !finePointer) return false;
    if (key === "Space")
      return (
        event.key === " " &&
        !element?.closest('button, a[href], [role="button"], [role="link"]')
      );
    if (key.startsWith("Key")) return event.code === key;
    if (key === "0–9") return /^[0-9]$/.test(event.key);
    if (key === "1–9") return /^[1-9]$/.test(event.key);
    return event.key === key;
  });
}

export function handleComposerShortcut(
  event: ShortcutEvent,
  text: string,
  pending: boolean,
  send: () => void,
) {
  if (pending || !text.trim() || !matchesShortcut("composer_send", event))
    return false;
  event.preventDefault();
  event.stopPropagation();
  send();
  return true;
}
