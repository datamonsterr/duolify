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
import {
  friendFailure,
  friendMessages,
  inviteUrl,
  linkPath,
  requestsPath,
  type FriendFailure,
  type SendResult,
} from "./friends";
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
  const [sent, setSent] = useState<Record<string, SendResult["status"]>>({});
  const [refused, setRefused] = useState<{
    username: string;
    failure: FriendFailure;
  }>();
  const [link, setLink] = useState<{
    url: string;
    days: number;
    copied: boolean;
  }>();
  const [linkProblem, setLinkProblem] = useState<FriendFailure>();
  const [linkBusy, setLinkBusy] = useState(false);
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

  async function send(username: string) {
    setRefused(undefined);
    try {
      const result = await session.request<SendResult>(requestsPath, "POST", {
        username,
      });
      setSent((all) => ({ ...all, [username]: result.status }));
    } catch (error) {
      setRefused({ username, failure: friendFailure(error) });
    }
  }

  async function createLink() {
    setLinkBusy(true);
    setLinkProblem(undefined);
    try {
      const made = await session.request<{
        token: string;
        valid_days?: number;
      }>(linkPath, "POST");
      const url = inviteUrl(location.origin, made.token);
      let copied = true;
      try {
        await navigator.clipboard.writeText(url);
      } catch {
        copied = false;
      }
      setLink({ url, days: made.valid_days ?? 30, copied });
    } catch (error) {
      setLinkProblem(friendFailure(error));
    } finally {
      setLinkBusy(false);
    }
  }

  async function revokeLink() {
    setLinkBusy(true);
    setLinkProblem(undefined);
    try {
      await session.request(linkPath, "DELETE");
      setLink(undefined);
    } catch (error) {
      setLinkProblem(friendFailure(error));
    } finally {
      setLinkBusy(false);
    }
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
                  {sent[person.username] ? (
                    <span role="status" className="social-chip">
                      {sent[person.username] === "friends"
                        ? "Friends"
                        : "Request sent"}
                    </span>
                  ) : (
                    <button
                      type="button"
                      className="duo-button secondary"
                      aria-label={`Send request to ${person.username}`}
                      onClick={() => void send(person.username)}
                    >
                      Send request
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
        {refused && (
          <p role="alert" className="social-error">
            {friendMessages[refused.failure]}
          </p>
        )}
        <section className="social-link" aria-labelledby={`${id}-link`}>
          <h3 id={`${id}-link`}>Or invite with a link</h3>
          {link ? (
            <>
              <label htmlFor={`${id}-url`}>Invite link</label>
              <input
                id={`${id}-url`}
                readOnly
                value={link.url}
                onFocus={(e) => e.currentTarget.select()}
              />
              <p role="status" className="social-hint">
                {link.copied
                  ? "Link copied"
                  : "Select the link above to copy it."}
              </p>
              <p className="social-hint">
                Valid for {link.days} days. Anyone with it can send you a
                request.
              </p>
              <button
                type="button"
                className="duo-button quiet"
                disabled={linkBusy}
                onClick={() => void revokeLink()}
              >
                Turn off link
              </button>
            </>
          ) : (
            <button
              type="button"
              className="duo-button secondary"
              disabled={linkBusy}
              onClick={() => void createLink()}
            >
              Copy invite link
            </button>
          )}
          {linkProblem && (
            <p role="alert" className="social-error">
              {friendMessages[linkProblem]}
            </p>
          )}
        </section>
        <div className="social-actions">
          <button type="button" className="duo-button quiet" onClick={onClose}>
            Close
          </button>
        </div>
      </form>
    </dialog>
  );
}
