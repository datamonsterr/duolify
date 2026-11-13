import { useEffect, useId, useRef, useState } from "react";
import { session } from "../accounts/session";
import {
  minSearch,
  normalizeSearch,
  searchFailure,
  searchMessages,
  searchPath,
  type FoundLearner,
  type SearchFailure,
} from "./search";
import "./social.css";

type Outcome =
  | { query: string; kind: "ready"; results: FoundLearner[] }
  | { query: string; kind: "error"; failure: SearchFailure };

/** Add a friend sheet (S14): bounded username search with neutral outcomes. */
export function AddFriend({ onClose }: { onClose: () => void }) {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const immediate = useRef(false);
  const [raw, setRaw] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [outcome, setOutcome] = useState<Outcome>();
  const query = normalizeSearch(raw);
  const searchable = [...query].length >= minSearch;

  useEffect(() => {
    const node = dialog.current;
    if (!node) return;
    const opener = document.activeElement;
    if (typeof node.showModal === "function" && !node.open) node.showModal();
    else node.setAttribute("open", "");
    input.current?.focus();
    return () => {
      if (typeof node.close === "function" && node.open) node.close();
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, []);

  useEffect(() => {
    if (!searchable) return;
    let current = true;
    const wait = immediate.current ? 0 : 250;
    immediate.current = false;
    const timer = setTimeout(() => {
      session
        .request<{ results: FoundLearner[] }>(searchPath(query))
        .then((value) => {
          if (current)
            setOutcome({ query, kind: "ready", results: value.results });
        })
        .catch((error: unknown) => {
          if (current)
            setOutcome({ query, kind: "error", failure: searchFailure(error) });
        });
    }, wait);
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [query, searchable, attempt]);

  // Results belong to the query that produced them; anything else is loading.
  const shown = searchable && outcome?.query === query ? outcome : undefined;
  const loading = searchable && !shown;

  function search(event: React.FormEvent) {
    event.preventDefault();
    if (!searchable) return;
    immediate.current = true;
    setOutcome(undefined);
    setAttempt((x) => x + 1);
  }

  return (
    <dialog
      ref={dialog}
      className="social-sheet"
      aria-labelledby={`${id}-title`}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      <form onSubmit={search} noValidate className="social-sheet-body">
        <h2 id={`${id}-title`}>Add a friend</h2>
        <label htmlFor={`${id}-input`}>Username</label>
        <input
          id={`${id}-input`}
          ref={input}
          value={raw}
          onChange={(event) => setRaw(event.target.value)}
          autoComplete="off"
          autoCapitalize="none"
          spellCheck={false}
          aria-describedby={`${id}-hint`}
        />
        <p id={`${id}-hint`} className="social-hint">
          At least {minSearch} characters
        </p>
        <div aria-live="polite" aria-busy={loading} className="social-results">
          {loading && <p>Searching…</p>}
          {shown?.kind === "error" && (
            <div role="alert" className="social-error">
              <p>{searchMessages[shown.failure]}</p>
              <button
                type="button"
                className="duo-button quiet"
                onClick={() => {
                  immediate.current = true;
                  setOutcome(undefined);
                  setAttempt((x) => x + 1);
                }}
              >
                Retry
              </button>
            </div>
          )}
          {shown?.kind === "ready" && shown.results.length === 0 && (
            <div>
              <strong>No one found with that username</strong>
              <p>Check the spelling, or share an invite link instead.</p>
            </div>
          )}
          {shown?.kind === "ready" && shown.results.length > 0 && (
            <ul aria-label="Search results" className="social-result-list">
              {shown.results.map((person) => (
                <li key={person.username}>
                  <span className="social-avatar small" aria-hidden="true">
                    {person.avatar.initial}
                  </span>
                  <span className="social-card-text">
                    <strong>{person.display_name}</strong>
                    <span>@{person.username}</span>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
        <div className="social-actions">
          <button type="button" className="duo-button quiet" onClick={onClose}>
            Close
          </button>
        </div>
      </form>
    </dialog>
  );
}
