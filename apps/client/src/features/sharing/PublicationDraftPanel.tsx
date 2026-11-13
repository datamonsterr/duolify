import { useCallback, useEffect, useId, useRef, useState } from "react";
import {
  draftFailure,
  draftMessages,
  drafts,
  findingLabels,
  verdictLabels,
  type DraftFailure,
  type PublicationDraft,
} from "./drafts";
import "./sharing.css";

type State =
  | { kind: "loading"; text: string }
  | { kind: "ready"; draft: PublicationDraft }
  | { kind: "failed"; failure: DraftFailure };

/** Owner-only preview of a screened publication draft. Desktop dialog, mobile sheet. */
export function PublicationDraftPanel({
  courseId,
  onClose,
}: {
  courseId: string;
  onClose: () => void;
}) {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  const [state, setState] = useState<State>({
    kind: "loading",
    text: "Building your draft…",
  });
  const [online, setOnline] = useState(
    typeof navigator === "undefined" ? true : navigator.onLine,
  );

  const run = useCallback((work: () => Promise<PublicationDraft>) => {
    let active = true;
    Promise.resolve()
      .then(work)
      .then((draft) => active && setState({ kind: "ready", draft }))
      .catch(
        (error: unknown) =>
          active && setState({ kind: "failed", failure: draftFailure(error) }),
      );
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => run(() => drafts.build(courseId)), [courseId, run]);

  useEffect(() => {
    const on = () => setOnline(true);
    const off = () => setOnline(false);
    window.addEventListener("online", on);
    window.addEventListener("offline", off);
    return () => {
      window.removeEventListener("online", on);
      window.removeEventListener("offline", off);
    };
  }, []);

  useEffect(() => {
    const node = dialog.current;
    if (!node) return;
    const opener = document.activeElement;
    if (typeof node.showModal === "function" && !node.open) node.showModal();
    else node.setAttribute("open", "");
    close.current?.focus();
    return () => {
      if (typeof node.close === "function" && node.open) node.close();
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, []);

  const draft = state.kind === "ready" ? state.draft : null;
  return (
    <dialog
      ref={dialog}
      className="sharing-sheet"
      aria-labelledby={`${id}-title`}
      data-testid="publication-draft-panel"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") onClose();
        if (event.key !== "Tab") return;
        // Wrap focus inside the sheet; native modal focus can otherwise leave the page.
        const items = [
          ...event.currentTarget.querySelectorAll<HTMLElement>(
            "button:not([disabled]), a[href], [tabindex]:not([tabindex='-1'])",
          ),
        ];
        const first = items[0];
        const last = items[items.length - 1];
        if (!first || !last) return;
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }}
    >
      <div className="sharing-body">
        <header className="sharing-head">
          <h2 id={`${id}-title`}>Preview publication draft</h2>
          <button
            ref={close}
            type="button"
            className="duo-button quiet"
            onClick={onClose}
          >
            Close
          </button>
        </header>
        <p className="sharing-note">
          Shared: course content only. Never: your profile, notes, chat,
          progress or steering instructions.
        </p>
        {!online && (
          <p role="status" className="sharing-banner">
            You're offline. Reconnect to build or screen a draft.
          </p>
        )}
        {state.kind === "loading" && (
          <p role="status" className="sharing-loading">
            <span className="sharing-spinner" aria-hidden="true" />
            {state.text}
          </p>
        )}
        {state.kind === "failed" && (
          <div className="sharing-error" role="alert">
            <p>{draftMessages[state.failure]}</p>
            {state.failure === "offline" && (
              <button
                type="button"
                className="duo-button secondary"
                onClick={() => {
                  setState({ kind: "loading", text: "Building your draft…" });
                  run(() => drafts.build(courseId));
                }}
              >
                Try again
              </button>
            )}
          </div>
        )}
        {draft && (
          <DraftView
            draft={draft}
            onRetry={() => {
              setState({ kind: "loading", text: "Screening again…" });
              run(() => drafts.retry(courseId, draft.id));
            }}
          />
        )}
        <button
          type="button"
          className="duo-button secondary"
          disabled
          aria-describedby={`${id}-later`}
        >
          Public sharing is not available yet
        </button>
        <p id={`${id}-later`} className="sharing-note">
          This draft stays private. Nothing is public and no link exists.
        </p>
      </div>
    </dialog>
  );
}

function DraftView({
  draft,
  onRetry,
}: {
  draft: PublicationDraft;
  onRetry: () => void;
}) {
  return (
    <div className="sharing-draft" data-testid="publication-draft">
      <div className="sharing-verdict-row">
        <span
          className="sharing-verdict"
          data-verdict={draft.verdict}
          data-testid="draft-verdict"
        >
          {verdictLabels[draft.verdict]}
        </span>
        <span className="sharing-meta">
          Draft {draft.number} · {draft.lesson_count} lessons · checked{" "}
          {draft.sampled_lessons}
        </span>
      </div>
      {draft.verdict === "pending_screening" && (
        <div className="sharing-pending">
          <p>
            Screening is unavailable right now. Learning is not affected, and
            nothing is shared.
          </p>
          <button
            type="button"
            className="duo-button primary"
            onClick={onRetry}
          >
            Retry screening
          </button>
        </div>
      )}
      {draft.verdict === "block" && (
        <p>
          This draft can't be shared. Edit the course content and build a new
          draft.
        </p>
      )}
      {draft.verdict === "needs_review" && (
        <p>Review the items below, then build a new draft.</p>
      )}
      {draft.findings.length > 0 && (
        <ul className="sharing-findings" aria-label="Findings">
          {draft.findings.map((f, i) => (
            <li key={`${f.kind}-${f.location}-${i}`}>
              <strong>{findingLabels[f.kind]}</strong>
              <span>{f.location}</span>
            </li>
          ))}
        </ul>
      )}
      {draft.outline.length === 0 ? (
        <p>No generated lessons yet. Generate a lesson to build a draft.</p>
      ) : (
        <ol className="sharing-outline" aria-label="Outline">
          {draft.outline.map((unit, i) => (
            <li key={`${unit.title}-${i}`}>
              <bdi>{unit.title}</bdi>
              <ul>
                {unit.lessons.map((lesson, j) => (
                  <li key={`${lesson.title}-${j}`}>
                    <bdi>{lesson.title}</bdi>
                  </li>
                ))}
              </ul>
            </li>
          ))}
        </ol>
      )}
      <p className="sharing-note">
        Narration audio is not included. It is regenerated for each learner.
      </p>
    </div>
  );
}
