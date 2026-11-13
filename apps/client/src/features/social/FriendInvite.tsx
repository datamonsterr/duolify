import { useCallback, useEffect, useState } from "react";
import { session } from "../accounts/session";
import {
  friendFailure,
  friendMessages,
  linkPath,
  parseInviteToken,
  requestsPath,
  type FriendFailure,
  type SendResult,
} from "./friends";
import "./social.css";

interface Inviter {
  username: string;
  display_name: string;
  avatar: { initial: string };
}
type State =
  | { kind: "checking" }
  | { kind: "ready"; inviter: Inviter }
  | { kind: "sent"; inviter: Inviter; status: SendResult["status"] }
  | { kind: "error"; failure: FriendFailure };

/**
 * Landing for /friends/invite/{token}. The route guard has already signed the
 * learner in; nothing is created until they confirm.
 */
export function FriendInvite({ token }: { token: string }) {
  const valid = parseInviteToken(token);
  const [state, setState] = useState<State>(
    valid ? { kind: "checking" } : { kind: "error", failure: "invite" },
  );
  const [busy, setBusy] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!valid) return;
    let current = true;
    session
      .request<Inviter>(`${linkPath}/resolve`, "POST", { token: valid })
      .then((inviter) => current && setState({ kind: "ready", inviter }))
      .catch(
        (error: unknown) =>
          current && setState({ kind: "error", failure: friendFailure(error) }),
      );
    return () => {
      current = false;
    };
  }, [valid, attempt]);

  const confirm = useCallback(
    async (inviter: Inviter) => {
      setBusy(true);
      try {
        const result = await session.request<SendResult>(requestsPath, "POST", {
          token: valid,
        });
        setState({ kind: "sent", inviter, status: result.status });
      } catch (error) {
        setState({ kind: "error", failure: friendFailure(error) });
      } finally {
        setBusy(false);
      }
    },
    [valid],
  );

  return (
    <section className="social-page" aria-labelledby="invite-title">
      <h1 id="invite-title" tabIndex={-1}>
        Friend invite
      </h1>
      {state.kind === "checking" && <p role="status">Checking invite…</p>}
      {state.kind === "error" && (
        <div role="alert" className="social-error">
          <p>{friendMessages[state.failure]}</p>
          {state.failure === "offline" || state.failure === "limited" ? (
            <button
              type="button"
              className="duo-button quiet"
              onClick={() => {
                setState({ kind: "checking" });
                setAttempt((x) => x + 1);
              }}
            >
              Retry
            </button>
          ) : (
            <a href="/friends" className="duo-button quiet">
              Go to Friends
            </a>
          )}
        </div>
      )}
      {(state.kind === "ready" || state.kind === "sent") && (
        <div className="social-card">
          <span className="social-avatar" aria-hidden="true">
            {state.inviter.avatar.initial}
          </span>
          <span className="social-card-text">
            <strong>{state.inviter.display_name}</strong>
            <span>@{state.inviter.username}</span>
          </span>
        </div>
      )}
      {state.kind === "ready" && (
        <>
          <p>
            invited you to be friends. They only see your username after you
            accept.
          </p>
          <button
            type="button"
            className="duo-button primary"
            disabled={busy}
            onClick={() => void confirm(state.inviter)}
          >
            Send friend request
          </button>
        </>
      )}
      {state.kind === "sent" && (
        <>
          <p role="status">
            {state.status === "friends" ? "You're friends now" : "Request sent"}
          </p>
          <a href="/friends" className="duo-button secondary">
            Go to Friends
          </a>
        </>
      )}
    </section>
  );
}
