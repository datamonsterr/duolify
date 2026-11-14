import { useEffect, useId, useRef, useState, type RefObject } from "react";
import {
  isSevereAction,
  moderationFailure,
  type ModerationAction,
} from "./moderation";

const focusable =
  "button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled)";

type Props = {
  title: string;
  reasonLabel: string;
  submitLabel: string;
  maxChars: number;
  help?: string;
  /** Closed reason codes; omitted for an appeal and for an appeal decision. */
  codes?: { id: string; label: string }[];
  initialCode?: string;
  /** Shows the "also block existing copies" preview for a delist or takedown. */
  action?: ModerationAction;
  returnFocus: RefObject<HTMLElement | null>;
  onSubmit: (code: string, text: string) => Promise<void>;
  onClose: () => void;
};

/**
 * One dialog for every reason-bearing step: moderator actions, appeal decisions and the author's
 * appeal. A centred dialog on desktop and a bottom sheet on a phone. The text survives every failure.
 */
export function ReasonDialog(p: Props) {
  const [code, setCode] = useState(p.initialCode ?? p.codes?.[0]?.id ?? "");
  const [text, setText] = useState("");
  const [failure, setFailure] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const sending = useRef(false);
  const root = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const helpId = useId();
  const trimmed = text.trim();
  const chars = Array.from(text).length;
  const severe = p.action ? isSevereAction(p.action, code) : false;
  const retryable =
    failure !== undefined &&
    [undefined, 429, 500, 502, 503].includes(
      (failure as { status?: number }).status,
    );

  useEffect(() => {
    root.current?.querySelector<HTMLElement>("select, textarea")?.focus();
  }, []);
  const close = () => {
    if (sending.current) return;
    p.onClose();
    // The opener keeps focus after the dialog closes (keyboard and screen-reader users).
    queueMicrotask(() => p.returnFocus.current?.focus());
  };
  const submit = async () => {
    if (sending.current || !trimmed || chars > p.maxChars) return;
    sending.current = true;
    setBusy(true);
    setFailure(undefined);
    try {
      await p.onSubmit(code, trimmed);
    } catch (error) {
      setFailure(error ?? new Error("failed"));
    } finally {
      sending.current = false;
      setBusy(false);
    }
  };

  return (
    <div className="report-layer">
      <div className="report-scrim" onClick={close} aria-hidden="true" />
      <div
        ref={root}
        className="report-sheet mod-dialog"
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
          <h2 id={titleId}>{p.title}</h2>
          <button
            type="button"
            className="report-close"
            aria-label="Close"
            onClick={close}
          >
            <span aria-hidden="true">×</span>
          </button>
        </div>
        <form
          className="report-form"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          {p.codes && (
            <label className="mod-field">
              Reason code
              <select
                value={code}
                disabled={busy}
                onChange={(e) => setCode(e.target.value)}
              >
                {p.codes.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.label}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label className="mod-field">
            {p.reasonLabel}
            <textarea
              rows={3}
              dir="auto"
              value={text}
              disabled={busy}
              aria-describedby={helpId}
              aria-invalid={chars > p.maxChars}
              onChange={(e) => setText(e.target.value)}
            />
          </label>
          <p className="sharing-note" id={helpId}>
            {chars} / {p.maxChars}. {p.help}
          </p>
          {(p.action === "takedown" || p.action === "delist") && (
            <div className="mod-block">
              <label className="mod-check">
                <input
                  type="checkbox"
                  checked={severe}
                  disabled
                  readOnly
                  aria-describedby={`${helpId}-block`}
                />
                Also block existing copies
              </label>
              <span className="sharing-note" id={`${helpId}-block`}>
                Only a takedown for an unsafe or legal reason blocks copies. Any
                other action keeps them working.
              </span>
            </div>
          )}
          {failure !== undefined && (
            <div className="sharing-error" role="alert">
              <p>{moderationFailure(failure)}</p>
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
              type="button"
              className="duo-button secondary"
              onClick={close}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="duo-button"
              disabled={busy || !trimmed || chars > p.maxChars}
            >
              {busy ? "Sending…" : p.submitLabel}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
