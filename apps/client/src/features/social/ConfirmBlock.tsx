import { useEffect, useId, useRef } from "react";
import "./social.css";

/** Names the person and the consequences before a block; friends can be removed without blocking. */
export function ConfirmBlock({
  name,
  busy,
  onCancel,
  onConfirm,
  onRemoveInstead,
}: {
  name: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  /** Only for a current friend: ends the friendship and nothing else. */
  onRemoveInstead?: () => void;
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
        <h2 id={`${id}-title`}>Block {name}?</h2>
        <p>
          {name} won't be able to find you, send requests or invites. Any friend
          streak ends. {name} isn't notified.
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
            Block
          </button>
        </div>
        {onRemoveInstead && (
          <button
            type="button"
            className="duo-button quiet"
            disabled={busy}
            onClick={onRemoveInstead}
          >
            Only remove friend
          </button>
        )}
      </div>
    </dialog>
  );
}
