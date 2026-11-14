import { useCallback, useEffect, useId, useRef, useState } from "react";
import {
  generateNext,
  generationState,
  loadMissing,
  missingFailure,
  missingMessages,
  releasedMessage,
  requestQuote,
  type MissingFailure,
  type MissingQuote,
  type MissingStatus,
} from "./missing";
import "./sharing.css";

type Sheet =
  | null
  | { kind: "quoting" }
  | { kind: "quote"; quote: MissingQuote }
  | { kind: "error"; failure: MissingFailure };
type Run =
  | { kind: "idle" }
  | { kind: "running" }
  | { kind: "done" }
  | { kind: "problem"; text: string; action: string };

/**
 * End of the published content of a Course copy: ready/total, the explicit price-version quote,
 * mandatory consent, then generation through the gated pipeline at the learner's own cost.
 */
export function MissingLessons({
  courseId,
  pollMs = 1000,
  onChanged,
}: {
  courseId: string;
  pollMs?: number;
  onChanged?: () => void;
}) {
  const [status, setStatus] = useState<MissingStatus | null>(null);
  const [loadError, setLoadError] = useState<MissingFailure | null>(null);
  const [sheet, setSheet] = useState<Sheet>(null);
  const [run, setRun] = useState<Run>({ kind: "idle" });
  const trigger = useRef<HTMLButtonElement>(null);
  const alive = useRef(true);
  // Bumped on every open/close so a quote that resolves after Escape cannot reopen the sheet.
  const ask = useRef(0);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const refresh = useCallback(
    () =>
      loadMissing(courseId).then(
        (s) => {
          if (!alive.current) return s;
          setStatus(s);
          setLoadError(null);
          return s;
        },
        (error: unknown) => {
          if (alive.current) setLoadError(missingFailure(error));
          return null;
        },
      ),
    [courseId],
  );
  useEffect(() => {
    void refresh();
  }, [refresh]);

  // A generation started elsewhere (or before a reload) is followed until it ends.
  const generating = status?.state === "generating" && run.kind === "idle";
  useEffect(() => {
    if (!generating) return;
    const timer = setInterval(() => {
      void refresh().then((s) => {
        if (s && s.state !== "generating") onChanged?.();
      });
    }, pollMs);
    return () => clearInterval(timer);
  }, [generating, refresh, pollMs, onChanged]);

  const openQuote = () => {
    setRun({ kind: "idle" });
    setSheet({ kind: "quoting" });
    const mine = ++ask.current;
    const live = () => alive.current && ask.current === mine;
    requestQuote(courseId).then(
      (quote) => live() && setSheet({ kind: "quote", quote }),
      (error: unknown) => {
        if (!live()) return;
        const failure = missingFailure(error);
        if (failure === "locked" || failure === "in_progress") {
          setSheet(null);
          void refresh();
        } else setSheet({ kind: "error", failure });
      },
    );
  };
  const closeSheet = () => {
    ask.current++;
    setSheet(null);
    queueMicrotask(() => trigger.current?.focus());
  };

  const follow = (id: string) => {
    const tick = () =>
      generationState(id).then(
        async (g) => {
          if (!alive.current) return;
          if (g.state === "completed") {
            await refresh();
            if (!alive.current) return;
            setRun({ kind: "done" });
            onChanged?.();
          } else if (g.state === "failed") {
            await refresh();
            if (alive.current)
              setRun({
                kind: "problem",
                text: releasedMessage,
                action: "Try again",
              });
          } else setTimeout(tick, pollMs);
        },
        () => alive.current && setTimeout(tick, pollMs * 3),
      );
    void tick();
  };

  const generate = (quote: MissingQuote) => {
    ask.current++;
    setSheet(null);
    setRun({ kind: "running" });
    generateNext(courseId, quote).then(
      (g) => follow(g.generation_id),
      (error: unknown) => {
        if (!alive.current) return;
        const failure = missingFailure(error);
        setRun({
          kind: "problem",
          text: missingMessages[failure],
          action: "Review quote",
        });
        void refresh();
      },
    );
  };

  if (loadError)
    return (
      <section className="missing-lessons" aria-label="Lessons">
        <p role="alert">{missingMessages[loadError]}</p>
        <button
          type="button"
          className="duo-button secondary"
          onClick={() => void refresh()}
        >
          Retry
        </button>
      </section>
    );
  if (!status)
    return (
      <section className="missing-lessons" aria-label="Lessons">
        <p role="status">
          <span className="sharing-spinner" aria-hidden="true" />
          Checking your lessons…
        </p>
      </section>
    );

  return (
    <section className="missing-lessons" aria-label="Lessons">
      <p data-testid="missing-ready">
        {status.ready} of {status.total} lessons ready
      </p>
      {status.missing > 0 && (
        <p className="sharing-note">
          Ready lessons are free for you. The rest are generated as you learn
          and cost credits.
        </p>
      )}
      {status.state === "locked" && run.kind === "idle" && (
        <p className="sharing-note">{missingMessages.locked}</p>
      )}
      {(run.kind === "running" || generating) && (
        <p role="status" aria-live="polite">
          <span className="sharing-spinner" aria-hidden="true" />
          Preparing your lesson…
        </p>
      )}
      {run.kind === "done" && (
        <p role="status" data-testid="missing-status">
          Lesson ready
        </p>
      )}
      {run.kind === "problem" && (
        <div className="missing-problem">
          <p role="alert">{run.text}</p>
          <button type="button" className="duo-button" onClick={openQuote}>
            {run.action}
          </button>
        </div>
      )}
      {status.state === "ready" &&
        run.kind !== "running" &&
        run.kind !== "problem" && (
          <div className="sharing-actions">
            <button
              ref={trigger}
              type="button"
              className="duo-button"
              onClick={openQuote}
            >
              Review quote
            </button>
          </div>
        )}
      {sheet && (
        <QuoteSheet
          sheet={sheet}
          onClose={closeSheet}
          onRetry={openQuote}
          onGenerate={generate}
        />
      )}
    </section>
  );
}

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** Desktop dialog, mobile bottom sheet. Escape closes; focus stays inside until it does. */
function QuoteSheet({
  sheet,
  onClose,
  onRetry,
  onGenerate,
}: {
  sheet: NonNullable<Sheet>;
  onClose: () => void;
  onRetry: () => void;
  onGenerate: (quote: MissingQuote) => void;
}) {
  const titleId = useId();
  const root = useRef<HTMLDivElement>(null);
  const [agreed, setAgreed] = useState(false);
  useEffect(() => {
    root.current?.focus();
  }, [sheet.kind]);

  const keys = (event: React.KeyboardEvent) => {
    if (event.key === "Escape") {
      event.stopPropagation();
      onClose();
    } else if (event.key === "Tab" && root.current) {
      const items = [...root.current.querySelectorAll<HTMLElement>(FOCUSABLE)];
      if (!items.length) return;
      const first = items[0]!;
      const last = items[items.length - 1]!;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }
  };

  const quote = sheet.kind === "quote" ? sheet.quote : null;
  return (
    <div className="missing-backdrop">
      <div
        ref={root}
        className="missing-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        onKeyDown={keys}
      >
        <div className="sharing-head">
          <h2 id={titleId}>Generate next lesson</h2>
          <button
            type="button"
            className="duo-button secondary"
            aria-label="Close"
            onClick={onClose}
          >
            ×
          </button>
        </div>
        {sheet.kind === "quoting" && (
          <p role="status">
            <span className="sharing-spinner" aria-hidden="true" />
            Getting your quote…
          </p>
        )}
        {sheet.kind === "error" && (
          <div className="sharing-body">
            <p role="alert">{missingMessages[sheet.failure]}</p>
            <div className="sharing-actions">
              <button type="button" className="duo-button" onClick={onRetry}>
                Retry
              </button>
              <button
                type="button"
                className="duo-button secondary"
                onClick={onClose}
              >
                Cancel
              </button>
            </div>
          </div>
        )}
        {quote && (
          <div className="sharing-body">
            <dl className="missing-quote" data-testid="missing-quote">
              <div>
                <dt>Price version</dt>
                <dd data-testid="quote-version">
                  Price version {quote.pricing_version}
                </dd>
              </div>
              <div>
                <dt>Lesson content</dt>
                <dd data-testid="quote-content">
                  {quote.content_credits} credits
                </dd>
              </div>
              <div>
                <dt>Narration</dt>
                <dd data-testid="quote-audio">{quote.audio_credits} credits</dd>
              </div>
              <div>
                <dt>Total</dt>
                <dd data-testid="quote-total">
                  up to {quote.total_credits} credits
                </dd>
              </div>
              <div>
                <dt>Your balance</dt>
                <dd>{quote.balance} credits</dd>
              </div>
            </dl>
            <p className="sharing-note">
              You are charged only for what is delivered; unused credits are
              released. Lessons already in your course stay free.
            </p>
            {!quote.sufficient && (
              <div>
                <p role="alert">Needs {quote.total_credits} credits</p>
                <a className="duo-button secondary" href="/credits">
                  See credit options
                </a>
              </div>
            )}
            <label className="missing-consent">
              <input
                type="checkbox"
                checked={agreed}
                disabled={!quote.sufficient}
                onChange={(e) => setAgreed(e.target.checked)}
              />
              I agree to spend up to {quote.total_credits} credits on this
              lesson
            </label>
            <div className="sharing-actions">
              <button
                type="button"
                className="duo-button"
                disabled={!agreed || !quote.sufficient}
                onClick={() => onGenerate(quote)}
              >
                Generate lesson
              </button>
              <button
                type="button"
                className="duo-button secondary"
                onClick={onClose}
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
