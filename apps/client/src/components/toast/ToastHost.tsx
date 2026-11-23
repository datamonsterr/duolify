import { useEffect } from "react";
import { dismissToast, TOAST_TTL_MS, useToast } from "./toast-store";
import "./toast.css";

/** Mount once near the app or stage root. Never takes focus. */
export function ToastHost() {
  const toast = useToast();
  const id = toast?.id;
  const ttl = toast?.ttlMs ?? TOAST_TTL_MS;
  useEffect(() => {
    if (!id) return;
    const timer = setTimeout(() => dismissToast(id), ttl);
    return () => clearTimeout(timer);
  }, [id, ttl, toast]);
  return (
    <div
      className="ui-toast-host"
      data-testid="toast-host"
      aria-live={toast?.tone === "error" ? "assertive" : "polite"}
      aria-atomic="true"
    >
      {toast && (
        <div className="ui-toast" data-tone={toast.tone} data-testid="toast">
          <button
            type="button"
            className="ui-toast-text"
            data-testid="toast-dismiss"
            onClick={() => dismissToast(toast.id)}
          >
            {toast.message}
          </button>
          {toast.action && (
            <button
              type="button"
              className="ui-toast-action"
              data-testid="toast-action"
              onClick={() => {
                toast.action?.onAction();
                dismissToast(toast.id);
              }}
            >
              {toast.action.label}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
