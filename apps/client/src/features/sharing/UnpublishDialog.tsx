import { useEffect, useId, useRef, useState } from "react";
import { unpublish } from "./retention";
import "./sharing.css";
import "./market.css";

const focusable = "button:not(:disabled)";

function failureText(error: unknown): string {
  const status = (error as { status?: number })?.status;
  if (status === 404 || status === 400)
    return "This course isn't available any more.";
  if (status === 429) return "Too many requests. Wait a minute, then retry.";
  if (status === undefined)
    return "You're offline or Duolify can't be reached. Check your connection and retry.";
  return "Couldn't unpublish right now. Try again.";
}

/**
 * Confirms taking a course back to private. A centred dialog on desktop and a bottom sheet on a phone;
 * it starts on the safe choice and keeps its retry after an offline failure.
 */
export function UnpublishDialog({
  courseId,
  onDone,
  onClose,
}: {
  courseId: string;
  onDone: () => void;
  onClose: () => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const keep = useRef<HTMLButtonElement>(null);
  const sending = useRef(false);
  const titleId = useId();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  useEffect(() => keep.current?.focus(), []);

  const submit = async () => {
    if (sending.current) return;
    sending.current = true;
    setBusy(true);
    setFailure(undefined);
    try {
      await unpublish(courseId);
      onDone();
    } catch (error) {
      setFailure(error ?? new Error("failed"));
    } finally {
      sending.current = false;
      setBusy(false);
    }
  };
  const retryable =
    failure !== undefined &&
    [undefined, 429, 500, 502, 503].includes(
      (failure as { status?: number }).status,
    );

  return (
    <div className="report-layer">
      <div
        className="report-scrim"
        onClick={() => !busy && onClose()}
        aria-hidden="true"
      />
      <div
        ref={root}
        className="report-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-busy={busy}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            // Keep the share panel open underneath.
            e.preventDefault();
            e.stopPropagation();
            if (!busy) onClose();
            return;
          }
          if (e.key !== "Tab") return;
          const nodes = Array.from(
            root.current?.querySelectorAll<HTMLElement>(focusable) ?? [],
          );
          const first = nodes[0];
          const last = nodes[nodes.length - 1];
          if (e.shiftKey && document.activeElement === first) {
            e.preventDefault();
            last?.focus();
          } else if (!e.shiftKey && document.activeElement === last) {
            e.preventDefault();
            first?.focus();
          }
        }}
      >
        <div className="report-head">
          <h2 id={titleId}>Unpublish this course?</h2>
        </div>
        <ul className="sharing-note">
          <li>
            The share link stops working and the marketplace listing is removed.
          </li>
          <li>No one can start a new copy or accept an invite.</li>
          <li>
            Learners who already copied it keep their copy and their progress.
          </li>
        </ul>
        {failure !== undefined && (
          <div className="sharing-error" role="alert">
            <p>{failureText(failure)}</p>
            {retryable && (
              <button
                type="button"
                className="duo-button secondary"
                onClick={() => void submit()}
              >
                Retry
              </button>
            )}
          </div>
        )}
        <div className="mod-buttons">
          <button
            ref={keep}
            type="button"
            className="duo-button secondary"
            disabled={busy}
            onClick={onClose}
          >
            Keep published
          </button>
          <button
            type="button"
            className="duo-button"
            disabled={busy}
            onClick={() => void submit()}
          >
            {busy ? "Unpublishing…" : "Unpublish"}
          </button>
        </div>
      </div>
    </div>
  );
}
