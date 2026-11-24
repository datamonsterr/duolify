import { useEffect, useRef, type ReactNode } from "react";
import "./modal-dialog.css";

const focusable =
  'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex="0"]';

export type ModalDialogProps = {
  onClose: () => void;
  children: ReactNode;
  className?: string;
  "aria-label"?: string;
  "aria-labelledby"?: string;
  "aria-describedby"?: string;
};

/**
 * Shared modal for confirms and guided dialogs. Native `showModal()` makes the
 * page inert and contains Tab; this adds initial focus, Escape / scrim close and
 * focus restore to the opener (plus a Tab wrap for platforms without showModal).
 */
export function ModalDialog({
  onClose,
  children,
  className = "",
  ...aria
}: ModalDialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const close = useRef(onClose);
  useEffect(() => {
    close.current = onClose;
  });
  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    const opener =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    if (typeof node.showModal === "function") node.showModal();
    else node.setAttribute("open", "");
    if (!node.contains(document.activeElement))
      node.querySelector<HTMLElement>(focusable)?.focus();
    return () => {
      if (typeof node.close === "function" && node.open) node.close();
      else node.removeAttribute("open");
      if (opener?.isConnected) opener.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      {...aria}
      aria-modal="true"
      className={`modal-dialog ${className}`.trim()}
      onCancel={(event) => {
        event.preventDefault();
        close.current();
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) close.current();
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.stopPropagation();
          close.current();
        } else if (event.key === "Tab") {
          const nodes = [
            ...(ref.current?.querySelectorAll<HTMLElement>(focusable) ?? []),
          ];
          const first = nodes[0];
          const last = nodes[nodes.length - 1];
          if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last?.focus();
          } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first?.focus();
          }
        }
      }}
    >
      {children}
    </dialog>
  );
}
