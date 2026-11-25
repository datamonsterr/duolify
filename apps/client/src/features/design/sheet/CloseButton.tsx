import { forwardRef } from "react";
import "../chat/guided-chat.css";

/** The one 44 px × close control for every dialog and sheet. Icon only; the name is the aria-label. */
export const CloseButton = forwardRef<
  HTMLButtonElement,
  { onClick: () => void; label?: string }
>(function CloseButton({ onClick, label = "Close" }, ref) {
  return (
    <button
      ref={ref}
      type="button"
      className="guided-sheet-close"
      onClick={onClick}
      aria-label={label}
    >
      <span aria-hidden="true">×</span>
    </button>
  );
});
