import { useCallback, useEffect, useRef, useState } from "react";
import { friendsPath, type FriendEntry } from "../social/friends";
import { session } from "../accounts/session";
import {
  cleanName,
  inviteFailure,
  inviteMessages,
  invites,
  type InviteFailure,
  type InviteTarget,
  type SentInvite,
} from "./invites";

const statusLabel: Record<SentInvite["status"], string> = {
  pending: "Pending",
  accepted: "Accepted",
  declined: "Declined",
  expired: "Expired",
  revoked: "Revoked",
};

/** Invite section of the Share sheet: username field, friend picker, sent list. */
export function InviteFriends({
  courseId,
  shared,
  onShareFirst,
}: {
  courseId: string;
  shared: boolean;
  onShareFirst: () => void;
}) {
  const [friends, setFriends] = useState<FriendEntry[]>([]);
  const [sent, setSent] = useState<SentInvite[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState("");
  const [failure, setFailure] = useState<InviteFailure>();
  const [notice, setNotice] = useState("");
  // One key per target until a send settles, so an offline retry replays, never repeats.
  const keys = useRef(new Map<string, string>());

  const load = useCallback(() => {
    let active = true;
    Promise.all([
      session.request<{ friends: FriendEntry[] }>(friendsPath),
      invites.sent(courseId),
    ])
      .then(([f, s]) => {
        if (!active) return;
        setFriends(f.friends);
        setSent(s.invites);
      })
      .catch(() => active && setLoadFailed(true))
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, [courseId]);
  useEffect(() => (shared ? load() : undefined), [shared, load]);

  async function send(target: InviteTarget, key: string, done?: () => void) {
    setBusy(key);
    setFailure(undefined);
    setNotice("");
    const operation = keys.current.get(key) ?? crypto.randomUUID();
    keys.current.set(key, operation);
    try {
      await invites.send(courseId, target, operation);
      keys.current.delete(key);
      setNotice("Invite sent");
      done?.();
      const fresh = await invites.sent(courseId).catch(() => undefined);
      if (fresh) setSent(fresh.invites);
    } catch (error) {
      const kind = inviteFailure(error);
      if (kind !== "offline") keys.current.delete(key);
      setFailure(kind);
      if (kind === "duplicate")
        void invites
          .sent(courseId)
          .then((fresh) => setSent(fresh.invites))
          .catch(() => undefined);
    } finally {
      setBusy("");
    }
  }

  async function revoke(invite: SentInvite) {
    setBusy(`revoke-${invite.id}`);
    setFailure(undefined);
    setNotice("");
    try {
      await invites.revoke(invite.id);
      setSent((list) =>
        list.map((i) => (i.id === invite.id ? { ...i, status: "revoked" } : i)),
      );
    } catch (error) {
      const kind = inviteFailure(error);
      setFailure(kind);
      if (kind === "unavailable") load();
    } finally {
      setBusy("");
    }
  }

  const pending = new Set(
    sent.filter((i) => i.status === "pending").map((i) => i.recipient.username),
  );

  return (
    <section
      className="sharing-invites"
      aria-labelledby={`invite-heading-${courseId}`}
      data-testid="invite-section"
    >
      <h3 id={`invite-heading-${courseId}`}>Invite friends</h3>
      {!shared ? (
        <div className="sharing-banner" data-testid="invite-private-prompt">
          <p>
            Invites need a shared course. Set it to “Anyone with the link”
            first. Invited friends see only the course title until they accept.
          </p>
          <button
            type="button"
            className="duo-button secondary"
            onClick={onShareFirst}
          >
            Share with link first
          </button>
        </div>
      ) : (
        <>
          <form
            className="sharing-invite-form"
            onSubmit={(event) => {
              event.preventDefault();
              const username = cleanName(name);
              if (username)
                void send({ username }, `u:${username}`, () => setName(""));
            }}
          >
            <label>
              <span>Friend's username</span>
              <input
                value={name}
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                dir="auto"
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            <button
              type="submit"
              className="duo-button"
              disabled={busy !== "" || cleanName(name) === ""}
            >
              Send invite
            </button>
          </form>
          {failure && (
            <p className="sharing-error" role="alert">
              {inviteMessages[failure]}
            </p>
          )}
          {notice && (
            <p
              className="sharing-note"
              role="status"
              data-testid="invite-notice"
            >
              {notice}
            </p>
          )}
          {loading && (
            <p role="status">
              <span className="sharing-spinner" aria-hidden="true" />
              Loading friends and invites…
            </p>
          )}
          {loadFailed && (
            <div className="sharing-error" role="alert">
              <p>{inviteMessages.offline}</p>
              <button
                type="button"
                className="duo-button"
                onClick={() => {
                  setLoading(true);
                  setLoadFailed(false);
                  load();
                }}
              >
                Retry
              </button>
            </div>
          )}
          {!loading && !loadFailed && friends.length === 0 && (
            <p className="sharing-note">
              Add friends to invite them in one tap, or type a username above.
            </p>
          )}
          {friends.length > 0 && (
            <ul className="sharing-people" aria-label="Your friends">
              {friends.map((f) => (
                <li key={f.id}>
                  <span className="sharing-person">
                    <strong>
                      <bdi>{f.display_name}</bdi>
                    </strong>
                    <span>@{f.username}</span>
                  </span>
                  <button
                    type="button"
                    className="duo-button secondary"
                    data-testid={`invite-friend-${f.username}`}
                    aria-label={`Invite ${f.display_name} (@${f.username})`}
                    disabled={busy !== "" || pending.has(f.username)}
                    onClick={() =>
                      void send({ friendship_id: f.id }, `f:${f.id}`)
                    }
                  >
                    {pending.has(f.username) ? "Invited" : "Invite"}
                  </button>
                </li>
              ))}
            </ul>
          )}
          {sent.length > 0 && (
            <>
              <h4>Sent invites</h4>
              <ul className="sharing-people" aria-label="Sent invites">
                {sent.map((i) => (
                  <li
                    key={i.id}
                    data-testid={`sent-invite-${i.recipient.username}`}
                  >
                    <span className="sharing-person">
                      <strong>
                        <bdi>{i.recipient.display_name}</bdi>
                      </strong>
                      <span>
                        @{i.recipient.username} · {statusLabel[i.status]}
                      </span>
                    </span>
                    {i.status === "pending" && (
                      <button
                        type="button"
                        className="duo-button secondary"
                        aria-label={`Revoke invite for @${i.recipient.username}`}
                        disabled={busy !== ""}
                        onClick={() => void revoke(i)}
                      >
                        Revoke
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            </>
          )}
        </>
      )}
    </section>
  );
}
