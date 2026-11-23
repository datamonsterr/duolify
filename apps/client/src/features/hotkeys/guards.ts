export type ShortcutEvent = Pick<
  KeyboardEvent,
  | "altKey"
  | "code"
  | "ctrlKey"
  | "key"
  | "keyCode"
  | "metaKey"
  | "repeat"
  | "shiftKey"
  | "target"
  | "defaultPrevented"
  | "preventDefault"
  | "stopPropagation"
> & { isComposing?: boolean; nativeEvent?: KeyboardEvent };

export function isComposing(event: ShortcutEvent) {
  return (
    event.isComposing || event.nativeEvent?.isComposing || event.keyCode === 229
  );
}

export function matchesUnmodifiedKey(
  event: ShortcutEvent,
  key: string,
  allowShift = false,
) {
  return (
    event.key === key &&
    !event.defaultPrevented &&
    !isComposing(event) &&
    !event.repeat &&
    (allowShift || !event.shiftKey) &&
    !event.altKey &&
    !event.ctrlKey &&
    !event.metaKey
  );
}

export function targetElement(target: EventTarget | null) {
  return target instanceof Element ? target : null;
}

export function isTextTarget(
  target: EventTarget | null = document.activeElement,
) {
  const element = targetElement(target);
  const editable = element?.closest("[contenteditable]");
  return !!(
    element?.closest(
      'input, textarea, select, [role="textbox"], [role="combobox"], [role="searchbox"]',
    ) ||
    (editable && editable.getAttribute("contenteditable") !== "false")
  );
}
