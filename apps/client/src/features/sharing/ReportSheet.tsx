import { useEffect, useId, useRef, useState, type RefObject } from "react";
import {
  reportFailure,
  reportMaxChars,
  reportMessages,
  reportReasons,
  sendReport,
  type ReportFailure,
  type ReportTarget,
} from "./reports";

type State =
  | { kind: "form" }
  | { kind: "sending" }
  | { kind: "failed"; failure: ReportFailure }
  | { kind: "done"; merged: boolean };

const focusable =
  "button:not(:disabled), input:not(:disabled), textarea:not(:disabled)";

/**
 * One report sheet for every target: a centred dialog on desktop and a bottom sheet on a phone.
 * The reason and the details survive every failure, and nothing here ever shows who else reported.
 */
export function ReportSheet({
  target,
  title,
  onClose,
  returnFocus,
}: {
  target: ReportTarget;
  title: string;
  onClose: () => void;
  returnFocus?: RefObject<HTMLElement | null>;
}) {
  const [reason, setReason] = useState("");
  const [text, setText] = useState("");
  const [state, setState] = useState<State>({ kind: "form" });
  const sending = useRef(false);
  const root = useRef<HTMLDivElement>(null);
  const done = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const helpId = useId();
  const chars = Array.from(text).length;
  const busy = state.kind === "sending";

  useEffect(() => {
    const opener =
      returnFocus?.current ?? (document.activeElement as HTMLElement | null);
    root.current?.querySelector<HTMLInputElement>("input[type=radio]")?.focus();
    return () => opener?.focus();
  }, [returnFocus]);
  useEffect(() => {
    if (state.kind === "done") done.current?.focus();
  }, [state.kind]);

  const submit = async () => {
    if (sending.current || !reason || chars > reportMaxChars) return;
    sending.current = true;
    setState({ kind: "sending" });
    try {
      const result = await sendReport(target, reason, text.trim());
      setState({ kind: "done", merged: result.merged });
    } catch (error) {
      setState({ kind: "failed", failure: reportFailure(error) });
    } finally {
      sending.current = false;
    }
  };
  const close = () => {
    if (!sending.current) onClose();
  };
  const failure = state.kind === "failed" ? state.failure : undefined;

  return (
    <div className="report-layer">
      <div className="report-scrim" onClick={close} aria-hidden="true" />
      <div
        ref={root}
        className="report-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-busy={busy}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.stopPropagation();
            close();
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
          <h2 id={titleId}>{title}</h2>
          <button
            type="button"
            className="report-close"
            aria-label="Close"
            onClick={close}
          >
            <span aria-hidden="true">×</span>
          </button>
        </div>
        {state.kind === "done" ? (
          <div className="report-done">
            <div role="status" className="report-thanks">
              <strong>
                {state.merged
                  ? "You already reported this"
                  : "Thanks — report sent"}
              </strong>
              <p>
                {state.merged
                  ? "Your earlier report is still open. Nothing more is needed."
                  : "A moderator will review it. The creator is never told who reported."}
              </p>
            </div>
            <button
              ref={done}
              type="button"
              className="duo-button"
              onClick={onClose}
            >
              Done
            </button>
          </div>
        ) : (
          <form
            className="report-form"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <fieldset disabled={busy}>
              <legend>Reason</legend>
              {reportReasons.map((r) => (
                <label key={r.id} className="report-reason">
                  <input
                    type="radio"
                    name="report-reason"
                    value={r.id}
                    checked={reason === r.id}
                    onChange={() => setReason(r.id)}
                  />
                  {r.label}
                </label>
              ))}
            </fieldset>
            <textarea
              rows={3}
              aria-label="Details, optional"
              aria-describedby={helpId}
              aria-invalid={chars > reportMaxChars}
              value={text}
              disabled={busy}
              onChange={(e) => setText(e.target.value)}
            />
            <p className="sharing-note" id={helpId}>
              {chars} / {reportMaxChars}. Only moderators can read this.
            </p>
            {failure && (
              <div className="sharing-error" role="alert">
                <p>{reportMessages[failure]}</p>
                {(failure === "offline" || failure === "rate_limited") && (
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
            <button
              type="submit"
              className="duo-button report-send"
              disabled={busy || !reason || chars > reportMaxChars}
            >
              {busy ? "Sending…" : "Send report"}
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
