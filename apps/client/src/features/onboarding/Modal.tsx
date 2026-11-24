import { closeOnEscape } from "../hotkeys/escape";
import { useEffect, useRef, type ReactNode, type KeyboardEvent } from "react";

// Native top-layer modal follows the existing Sheet's focus/return pattern.
// The full viewport is inside the dialog so an SVG cutout can reveal real UI.
export function Modal({
  children,
  label,
  onSkip,
  onKey,
  testId,
  stepId,
  stepIndex,
  total,
}: {
  children: ReactNode;
  label: string;
  onSkip: () => void;
  onKey?: (event: KeyboardEvent<HTMLDialogElement>) => void;
  testId: string;
  stepId?: string;
  stepIndex?: number;
  total?: number;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const node = dialog.current!;
    const previous =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    node.showModal();
    node.querySelector<HTMLElement>("h2")?.focus({ preventScroll: true });
    return () => {
      node.close();
      if (previous?.isConnected) previous.focus({ preventScroll: true });
    };
  }, []);
  return (
    <dialog
      ref={dialog}
      className="onboarding-modal"
      aria-label={label}
      aria-modal="true"
      data-testid={testId}
      data-step-id={stepId}
      data-step-index={stepIndex}
      data-step-total={total}
      onCancel={(event) => {
        event.preventDefault();
        onSkip();
      }}
      onKeyDown={(event) => {
        if (closeOnEscape(event, onSkip, "learn")) return;
        if (event.key === "Tab") {
          const items = [
            ...dialog.current!.querySelectorAll<HTMLElement>(
              "button:not(:disabled),input:not(:disabled),a[href]",
            ),
          ].filter((node) => node.getBoundingClientRect().width > 0);
          const first = items[0],
            last = items[items.length - 1];
          const current = document.activeElement;
          if (
            event.shiftKey &&
            (current === first || !items.includes(current as HTMLElement))
          ) {
            event.preventDefault();
            last?.focus();
          } else if (
            !event.shiftKey &&
            (current === last || !items.includes(current as HTMLElement))
          ) {
            event.preventDefault();
            first?.focus();
          }
        }
        onKey?.(event);
      }}
    >
      {children}
    </dialog>
  );
}
