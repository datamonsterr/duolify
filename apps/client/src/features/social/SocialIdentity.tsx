import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { session } from "../accounts/session";
import {
  normalizeUsername,
  usernameHelp,
  usernameRules,
  validateUsername,
} from "./username";
import "./social.css";

interface PublicIdentity {
  username: string;
  display_name: string;
  avatar: { initial: string };
}
interface IdentityResponse {
  username: PublicIdentity | null;
  rules: { min: number; max: number };
}

/** Claim form and minimal public identity card (E6-01). `compact` is the Settings row. */
export function SocialIdentity({ compact = false }: { compact?: boolean }) {
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const [data, setData] = useState<IdentityResponse>();
  const [loadError, setLoadError] = useState("");
  const [revision, setRevision] = useState(0);
  const [draft, setDraft] = useState("");
  const [problem, setProblem] = useState("");
  const [retryable, setRetryable] = useState(false);
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState("");
  const changeButton = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef(false);

  useEffect(() => {
    if (editing) input.current?.focus();
    else if (returnFocus.current) {
      returnFocus.current = false;
      changeButton.current?.focus();
    }
  }, [editing]);

  useEffect(() => {
    let active = true;
    session
      .request<IdentityResponse>("/v1/social/identity")
      .then((value) => {
        if (active) {
          setData(value);
          setLoadError("");
        }
      })
      .catch((err: unknown) => {
        if (active)
          setLoadError(
            err instanceof Error ? err.message : "Couldn't load your username.",
          );
      });
    return () => {
      active = false;
    };
  }, [revision]);

  async function claim(event?: FormEvent, method: "PUT" | "PATCH" = "PUT") {
    event?.preventDefault();
    if (busy) return;
    const invalid = validateUsername(draft, data?.rules ?? usernameRules);
    if (invalid) {
      setProblem(invalid);
      setRetryable(false);
      input.current?.focus();
      return;
    }
    setBusy(true);
    setProblem("");
    try {
      const next = await session.request<IdentityResponse>(
        "/v1/social/username",
        method,
        {
          username: normalizeUsername(draft),
        },
      );
      setData(next);
      if (method === "PATCH") {
        setEditing(false);
        returnFocus.current = true;
        setNotice("Username changed.");
      }
    } catch (err) {
      const status = (err as { status?: number }).status;
      const rejected = status === 409 || status === 422;
      setProblem(
        err instanceof Error && err.message
          ? err.message
          : "Couldn't reach the server. Retry.",
      );
      setRetryable(!rejected);
      if (rejected) input.current?.focus();
    } finally {
      setBusy(false);
    }
  }

  if (loadError)
    return (
      <div className="social-identity" role="alert">
        <p>{loadError}</p>
        <button
          type="button"
          className="duo-button quiet"
          onClick={() => setRevision((x) => x + 1)}
        >
          Retry
        </button>
      </div>
    );
  if (!data)
    return (
      <p className="social-identity" role="status" aria-busy="true">
        Loading your username…
      </p>
    );
  const me = data.username;
  if (me && compact)
    return (
      <div className="social-rename-row">
        <p className="social-handle">
          <span>Username</span> <strong>@{me.username}</strong>
        </p>
        {editing ? (
          <form
            className="social-claim social-rename"
            aria-label="Change username"
            onSubmit={(event) => void claim(event, "PATCH")}
            noValidate
            aria-busy={busy}
          >
            <label htmlFor={`${id}-input`}>New username</label>
            <input
              id={`${id}-input`}
              ref={input}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              aria-invalid={problem ? true : undefined}
              aria-describedby={`${id}-help${problem ? ` ${id}-error` : ""}`}
            />
            <p id={`${id}-help`} className="social-hint">
              {usernameHelp} · You can change it once every 30 days and your old
              name is held for you.
            </p>
            {problem && (
              <div id={`${id}-error`} role="alert" className="social-error">
                {problem}
              </div>
            )}
            <div className="social-actions">
              <button
                type="submit"
                className="duo-button primary"
                disabled={busy}
              >
                Save username
              </button>
              {retryable && (
                <button
                  type="button"
                  className="duo-button quiet"
                  onClick={() => void claim(undefined, "PATCH")}
                >
                  Retry
                </button>
              )}
              <button
                type="button"
                className="duo-button quiet"
                onClick={() => {
                  setEditing(false);
                  setProblem("");
                  returnFocus.current = true;
                }}
              >
                Cancel
              </button>
            </div>
          </form>
        ) : (
          <>
            <button
              ref={changeButton}
              type="button"
              className="duo-button quiet"
              onClick={() => {
                setDraft(me.username);
                setProblem("");
                setNotice("");
                setEditing(true);
              }}
            >
              Change username
            </button>
            {notice && (
              <span role="status" aria-label="Username status">
                {notice}
              </span>
            )}
          </>
        )}
      </div>
    );
  if (me)
    return (
      <section className="social-card" aria-label="Your public identity">
        <span className="social-avatar" aria-hidden="true">
          {me.avatar.initial}
        </span>
        <div className="social-card-text">
          <strong className="social-handle-text">@{me.username}</strong>
          <span className="social-name">{me.display_name}</span>
          <small>
            Friends see only your username, display name and avatar.
          </small>
        </div>
      </section>
    );
  if (compact)
    return (
      <p className="social-handle">
        <span>Username</span> <Link to="/friends">Choose a username</Link>
      </p>
    );
  return (
    <form className="social-claim" onSubmit={claim} noValidate aria-busy={busy}>
      <h2>Choose your username</h2>
      <p id={`${id}-help`}>Friends find you by this name. {usernameHelp}</p>
      <label htmlFor={`${id}-input`}>Username</label>
      <input
        id={`${id}-input`}
        ref={input}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        autoComplete="off"
        autoCapitalize="none"
        spellCheck={false}
        inputMode="text"
        aria-invalid={problem ? true : undefined}
        aria-describedby={`${id}-help${problem ? ` ${id}-error` : ""}`}
      />
      {problem && (
        <div id={`${id}-error`} role="alert" className="social-error">
          {problem}
        </div>
      )}
      <div className="social-actions">
        <button type="submit" className="duo-button primary" disabled={busy}>
          Claim username
        </button>
        {retryable && (
          <button
            type="button"
            className="duo-button quiet"
            onClick={() => void claim()}
          >
            Retry
          </button>
        )}
      </div>
    </form>
  );
}
