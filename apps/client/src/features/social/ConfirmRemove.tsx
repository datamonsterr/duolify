import { useEffect, useId, useRef } from "react";
import "./social.css";

/** Names the person and the consequence before a friendship ends. */
export function ConfirmRemove({
  name,
  busy,
  onCancel,
  onConfirm,
}: {
  name: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const node = dialog.current;
    if (!node) return;
    const opener = document.activeElement;
    if (typeof node.showModal === "function" && !node.open) node.showModal();
    else node.setAttribute("open", "");
    cancel.current?.focus();
    return () => {
      if (typeof node.close === "function" && node.open) node.close();
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, []);
  return (
    <dialog
      ref={dialog}
      className="social-sheet confirm"
      aria-labelledby={`${id}-title`}
      onCancel={(event) => {
        event.preventDefault();
        onCancel();
      }}
    >
      <div className="social-sheet-body">
        <h2 id={`${id}-title`}>Remove {name}?</h2>
        <p>
          You'll both leave each other's friends list and any friend streak
          ends. {name} isn't notified.
        </p>
        <div className="social-actions">
          <button
            ref={cancel}
            type="button"
            className="duo-button secondary"
            onClick={onCancel}
          >
            Cancel
          </button>
          <button
            type="button"
            className="duo-button primary social-danger"
            disabled={busy}
            onClick={onConfirm}
          >
            Remove
          </button>
        </div>
      </div>
    </dialog>
  );
}
