import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { session } from "../accounts/session";
import { invites, type ReceivedInvite } from "../sharing/invites";
import { ReceivedInvites } from "../sharing/ReceivedInvites";
import { AddFriend } from "./AddFriend";
import { ConfirmBlock } from "./ConfirmBlock";
import { ConfirmRemove } from "./ConfirmRemove";
import {
  blockFailure,
  blockMessages,
  blocksPath,
  cleanUsername,
  type BlockedEntry,
  type BlockFailure,
} from "./blocks";
import {
  friendFailure,
  friendMessages,
  friendsPath,
  requestsPath,
  sinceLabel,
  type FriendEntry,
  type FriendFailure,
  type FriendRequests,
  type RequestEntry,
} from "./friends";
import { SocialIdentity } from "./SocialIdentity";
import { FriendStreakDetail, FriendStreakRow } from "./FriendStreak";
import { NudgeControl, NudgeInbox } from "./Nudge";
import {
  createNudgeKeys,
  nudgeFailure,
  nudgeMessage,
  nudgePath,
  nudgesPath,
  nudgesReadPath,
  unreadCount,
  type NudgeEntry,
  type NudgeFailure,
} from "./nudges";
import "./social.css";

type Tab = "friends" | "nudges" | "invites" | "requests" | "blocked";

/** Who the block dialog is about; `friend` is set only when they are a current friend. */
interface BlockTarget {
  name: string;
  username: string;
  friend?: FriendEntry;
}

function Person({
  person,
}: {
  person: {
    username: string;
    display_name: string;
    avatar: { initial: string };
  };
}) {
  return (
    <>
      <span className="social-avatar small" aria-hidden="true">
        {person.avatar.initial}
      </span>
      <span className="social-card-text">
        <strong>{person.display_name}</strong>
        <span>@{person.username}</span>
      </span>
    </>
  );
}

/** Friends (S14): list-detail on desktop, two screens on mobile; requests in a second tab. */
export function FriendsPage() {
  const id = useId();
  const [adding, setAdding] = useState(false);
  const [tab, setTab] = useState<Tab>("friends");
  const [friends, setFriends] = useState<FriendEntry[]>();
  const [requests, setRequests] = useState<FriendRequests>();
  const [failure, setFailure] = useState<FriendFailure>();
  const [selected, setSelected] = useState<string>();
  const [removing, setRemoving] = useState(false);
  const [busy, setBusy] = useState("");
  const [problem, setProblem] = useState<FriendFailure>();
  const [blocked, setBlocked] = useState<BlockedEntry[]>();
  const [blocking, setBlocking] = useState<BlockTarget>();
  const [blockProblem, setBlockProblem] = useState<BlockFailure>();
  const [typed, setTyped] = useState("");
  const [nudges, setNudges] = useState<NudgeEntry[]>();
  const [nudging, setNudging] = useState("");
  const [nudgeProblem, setNudgeProblem] = useState<{
    kind: NudgeFailure;
    name: string;
  }>();
  const keys = useRef(createNudgeKeys()).current;
  const navigate = useNavigate();
  const [courseInvites, setCourseInvites] = useState<ReceivedInvite[]>([]);
  const [inviteUnread, setInviteUnread] = useState(0);

  const [revision, setRevision] = useState(0);
  const load = useCallback(() => setRevision((x) => x + 1), []);
  useEffect(() => {
    let active = true;
    Promise.all([
      session.request<{ friends: FriendEntry[] }>(friendsPath),
      session.request<FriendRequests>(requestsPath),
      session.request<{ blocks?: BlockedEntry[] }>(blocksPath),
      session.request<{ nudges?: NudgeEntry[] }>(nudgesPath),
      // The invites inbox is optional here: a failure never hides friends.
      Promise.resolve(invites.received()).catch(() => undefined),
    ])
      .then(([f, r, b, n, inbox]) => {
        if (!active) return;
        setFriends(f.friends);
        setRequests(r);
        setBlocked(b.blocks ?? []);
        setNudges(n.nudges ?? []);
        setCourseInvites(inbox?.invites ?? []);
        setInviteUnread(inbox?.unread ?? 0);
        setFailure(undefined);
      })
      .catch((error: unknown) => {
        if (active) setFailure(friendFailure(error));
      });
    return () => {
      active = false;
    };
  }, [revision]);

  async function act(key: string, path: string, method: string) {
    setBusy(key);
    setProblem(undefined);
    try {
      await session.request(path, method);
    } catch (error) {
      setProblem(friendFailure(error));
    } finally {
      load();
      setBusy("");
    }
  }

  async function sendNudge(entry: FriendEntry) {
    setNudging(entry.id);
    setNudgeProblem(undefined);
    const spent = () =>
      setFriends((list) =>
        list?.map((f) =>
          f.id === entry.id ? { ...f, nudge: { sent: true } } : f,
        ),
      );
    try {
      await session.request(nudgePath(entry.id), "POST", {
        operation_key: keys.keyFor(entry.id),
      });
      keys.clear(entry.id);
      spent();
    } catch (error) {
      const kind = nudgeFailure(error);
      if (kind !== "offline") keys.clear(entry.id); // an offline retry replays the same key
      if (kind === "limited") spent();
      setNudgeProblem({ kind, name: entry.display_name });
      if (kind === "unavailable") load();
    } finally {
      setNudging("");
    }
  }

  async function openNudges() {
    setTab("nudges");
    if (!nudges || unreadCount(nudges) === 0) return;
    try {
      await session.request(nudgesReadPath, "POST");
      setNudges((list) => list?.map((n) => ({ ...n, read: true })));
    } catch {
      // The list stays unread and is retried the next time the tab opens.
    }
  }

  async function openInvites() {
    setTab("invites");
    if (inviteUnread === 0) return;
    try {
      await invites.markRead();
      setInviteUnread(0);
      setCourseInvites((list) => list.map((i) => ({ ...i, read: true })));
    } catch {
      // Stays unread and is retried the next time the tab opens.
    }
  }

  async function remove(friend: FriendEntry) {
    await act(`remove-${friend.id}`, `${friendsPath}/${friend.id}`, "DELETE");
    setRemoving(false);
    setSelected(undefined);
  }

  async function block(target: BlockTarget) {
    setBusy("block");
    setBlockProblem(undefined);
    try {
      await session.request(blocksPath, "POST", { username: target.username });
      setTyped("");
      setSelected(undefined);
    } catch (error) {
      setBlockProblem(blockFailure(error));
    } finally {
      setBlocking(undefined);
      load();
      setBusy("");
    }
  }

  async function unblock(entry: BlockedEntry) {
    setBusy(`unblock-${entry.username}`);
    setBlockProblem(undefined);
    try {
      await session.request(
        `${blocksPath}/${encodeURIComponent(entry.username)}`,
        "DELETE",
      );
    } catch (error) {
      setBlockProblem(blockFailure(error));
    } finally {
      load();
      setBusy("");
    }
  }

  const friend = friends?.find((f) => f.id === selected);
  const incoming = requests?.incoming ?? [];
  const outgoing = requests?.outgoing ?? [];
  const blockedList = blocked ?? [];
  const ready =
    friends !== undefined &&
    requests !== undefined &&
    blocked !== undefined &&
    nudges !== undefined;
  const requestRow = (entry: RequestEntry, sent: boolean) => (
    <li key={entry.id}>
      <Person person={entry} />
      <span className="social-row-actions">
        {sent ? (
          <>
            <span className="social-chip">Pending</span>
            <button
              type="button"
              className="duo-button quiet"
              aria-label={`Cancel request to ${entry.username}`}
              disabled={busy === entry.id}
              onClick={() =>
                act(entry.id, `${requestsPath}/${entry.id}`, "DELETE")
              }
            >
              Cancel
            </button>
          </>
        ) : (
          <>
            <button
              type="button"
              className="duo-button primary"
              aria-label={`Accept ${entry.username}`}
              disabled={busy === entry.id}
              onClick={() =>
                act(entry.id, `${requestsPath}/${entry.id}/accept`, "POST")
              }
            >
              Accept
            </button>
            <button
              type="button"
              className="duo-button secondary"
              aria-label={`Decline ${entry.username}`}
              disabled={busy === entry.id}
              onClick={() =>
                act(entry.id, `${requestsPath}/${entry.id}/decline`, "POST")
              }
            >
              Decline
            </button>
          </>
        )}
      </span>
    </li>
  );

  return (
    <section className="social-page friends-page">
      <h1 tabIndex={-1}>Friends</h1>
      <SocialIdentity />
      <div className="social-actions">
        <button
          type="button"
          className="duo-button primary"
          onClick={() => setAdding(true)}
        >
          Add
        </button>
      </div>
      {adding && (
        <AddFriend
          onClose={() => {
            setAdding(false);
            load();
          }}
        />
      )}
      {failure && (
        <div role="alert" className="social-error">
          <p>{friendMessages[failure]}</p>
          <button
            type="button"
            className="duo-button quiet"
            onClick={() => void load()}
          >
            Retry
          </button>
        </div>
      )}
      {!ready && !failure && <p role="status">Loading friends…</p>}
      {ready && (
        <>
          <div
            role="tablist"
            aria-label="Friends sections"
            className="friends-tabs"
          >
            <button
              type="button"
              role="tab"
              id={`${id}-tab-friends`}
              aria-selected={tab === "friends"}
              aria-controls={`${id}-panel`}
              onClick={() => setTab("friends")}
            >
              Friends · {friends.length}
            </button>
            <button
              type="button"
              role="tab"
              id={`${id}-tab-nudges`}
              aria-selected={tab === "nudges"}
              aria-controls={`${id}-panel`}
              onClick={() => void openNudges()}
            >
              Nudges · {unreadCount(nudges)}
            </button>
            <button
              type="button"
              role="tab"
              id={`${id}-tab-invites`}
              aria-selected={tab === "invites"}
              aria-controls={`${id}-panel`}
              onClick={() => void openInvites()}
            >
              Invites · {inviteUnread}
            </button>
            <button
              type="button"
              role="tab"
              id={`${id}-tab-requests`}
              aria-selected={tab === "requests"}
              aria-controls={`${id}-panel`}
              onClick={() => setTab("requests")}
            >
              Requests · {incoming.length}
            </button>
            <button
              type="button"
              role="tab"
              id={`${id}-tab-blocked`}
              aria-selected={tab === "blocked"}
              aria-controls={`${id}-panel`}
              onClick={() => setTab("blocked")}
            >
              Blocked · {blockedList.length}
            </button>
          </div>
          {problem && (
            <p role="alert" className="social-error">
              {friendMessages[problem]}
            </p>
          )}
          {nudgeProblem && (
            <p role="alert" className="social-error">
              {nudgeMessage(nudgeProblem.kind, nudgeProblem.name)}
            </p>
          )}
          {blockProblem && (
            <p role="alert" className="social-error">
              {blockMessages[blockProblem]}
            </p>
          )}
          <div
            role="tabpanel"
            id={`${id}-panel`}
            aria-labelledby={`${id}-tab-${tab}`}
            className="friends-panel"
          >
            {tab === "friends" &&
              (friends.length === 0 ? (
                <div className="friends-empty">
                  <h2>Learn with a friend</h2>
                  <p>
                    Friend streaks grow when you both finish a lesson on the
                    same day.
                  </p>
                  <button
                    type="button"
                    className="duo-button primary"
                    onClick={() => setAdding(true)}
                  >
                    Add a friend
                  </button>
                </div>
              ) : (
                <div
                  className="friends-layout"
                  data-detail={friend ? "open" : "closed"}
                >
                  <ul
                    aria-label="Your friends"
                    className="social-result-list friends-list"
                  >
                    {friends.map((entry) => (
                      <li key={entry.id}>
                        <button
                          type="button"
                          className="friends-open"
                          aria-label={`Open ${entry.display_name}`}
                          aria-current={entry.id === selected || undefined}
                          onClick={() => setSelected(entry.id)}
                        >
                          <Person person={entry} />
                        </button>
                        {entry.friend_streak && (
                          <FriendStreakRow
                            name={entry.display_name}
                            streak={entry.friend_streak}
                          />
                        )}
                        <NudgeControl
                          entry={entry}
                          busy={nudging === entry.id}
                          onNudge={() => void sendNudge(entry)}
                        />
                      </li>
                    ))}
                  </ul>
                  <section
                    aria-label="Friend details"
                    className="friends-detail"
                  >
                    {friend ? (
                      <>
                        <button
                          type="button"
                          className="duo-button quiet friends-back"
                          onClick={() => setSelected(undefined)}
                        >
                          Back to friends
                        </button>
                        <div className="social-card">
                          <span className="social-avatar" aria-hidden="true">
                            {friend.avatar.initial}
                          </span>
                          <span className="social-card-text">
                            <h2>{friend.display_name}</h2>
                            <span>@{friend.username}</span>
                          </span>
                        </div>
                        <p className="social-hint">
                          Friends since {sinceLabel(friend.since)}
                        </p>
                        {friend.friend_streak ? (
                          <FriendStreakDetail
                            name={friend.display_name}
                            streak={friend.friend_streak}
                          />
                        ) : (
                          friend.streak && (
                            <p>{friend.streak.days}-day streak</p>
                          )
                        )}
                        <NudgeControl
                          entry={friend}
                          busy={nudging === friend.id}
                          onNudge={() => void sendNudge(friend)}
                        />
                        <button
                          type="button"
                          className="duo-button secondary"
                          onClick={() => setRemoving(true)}
                        >
                          Remove friend
                        </button>
                        <button
                          type="button"
                          className="duo-button secondary"
                          onClick={() =>
                            setBlocking({
                              name: friend.display_name,
                              username: friend.username,
                              friend,
                            })
                          }
                        >
                          Block
                        </button>
                      </>
                    ) : (
                      <p className="social-hint">
                        Choose a friend to see details.
                      </p>
                    )}
                  </section>
                </div>
              ))}
            {tab === "nudges" && (
              <div className="friends-requests">
                <h2>Nudges</h2>
                <p className="social-hint">
                  Friends can nudge you once a day. Nudges stay in the app for a
                  couple of weeks.
                </p>
                <NudgeInbox items={nudges} />
              </div>
            )}
            {tab === "invites" && (
              <div className="friends-requests">
                <h2>Course invites</h2>
                <p className="social-hint">
                  Friends can invite you to a shared course. You see only its
                  title until you accept. Invites expire after 30 days.
                </p>
                <ReceivedInvites
                  items={courseInvites}
                  onResolved={(invite) =>
                    setCourseInvites((list) =>
                      list.filter((i) => i.id !== invite),
                    )
                  }
                  onAccepted={(courseId) =>
                    void navigate({
                      to: "/course/$courseId",
                      params: { courseId },
                    })
                  }
                />
              </div>
            )}
            {tab === "blocked" && (
              <div className="friends-requests">
                <h2>Blocked</h2>
                <p className="social-hint">
                  People you block can't find you or send requests or invites,
                  and aren't told. Unblocking doesn't bring back a friendship.
                </p>
                <form
                  className="social-actions"
                  onSubmit={(event) => {
                    event.preventDefault();
                    const username = cleanUsername(typed);
                    if (username)
                      setBlocking({ name: `@${username}`, username });
                  }}
                >
                  <label>
                    <span>Username to block</span>
                    <input
                      value={typed}
                      autoComplete="off"
                      autoCapitalize="none"
                      spellCheck={false}
                      maxLength={64}
                      onChange={(event) => setTyped(event.target.value)}
                    />
                  </label>
                  <button
                    type="submit"
                    className="duo-button secondary"
                    disabled={!cleanUsername(typed)}
                  >
                    Block someone
                  </button>
                </form>
                {blockedList.length === 0 ? (
                  <p className="social-hint">You haven't blocked anyone.</p>
                ) : (
                  <ul
                    aria-label="Blocked learners"
                    className="social-result-list"
                  >
                    {blockedList.map((entry) => (
                      <li key={entry.username}>
                        <Person person={entry} />
                        <span className="social-row-actions">
                          <button
                            type="button"
                            className="duo-button secondary"
                            aria-label={`Unblock ${entry.username}`}
                            disabled={busy === `unblock-${entry.username}`}
                            onClick={() => void unblock(entry)}
                          >
                            Unblock
                          </button>
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
            {tab === "requests" && (
              <div className="friends-requests">
                <h2>Friend requests</h2>
                {incoming.length === 0 ? (
                  <p className="social-hint">No requests right now.</p>
                ) : (
                  <ul
                    aria-label="Friend requests"
                    className="social-result-list"
                  >
                    {incoming.map((entry) => requestRow(entry, false))}
                  </ul>
                )}
                <h2>Sent</h2>
                {outgoing.length === 0 ? (
                  <p className="social-hint">Nothing waiting for a reply.</p>
                ) : (
                  <ul aria-label="Sent requests" className="social-result-list">
                    {outgoing.map((entry) => requestRow(entry, true))}
                  </ul>
                )}
              </div>
            )}
          </div>
        </>
      )}
      {blocking && (
        <ConfirmBlock
          name={blocking.name}
          busy={busy === "block"}
          onCancel={() => setBlocking(undefined)}
          onConfirm={() => void block(blocking)}
          onRemoveInstead={
            blocking.friend
              ? () => {
                  const target = blocking.friend!;
                  setBlocking(undefined);
                  void remove(target);
                }
              : undefined
          }
        />
      )}
      {removing && friend && (
        <ConfirmRemove
          name={friend.display_name}
          busy={busy === `remove-${friend.id}`}
          onCancel={() => setRemoving(false)}
          onConfirm={() => void remove(friend)}
        />
      )}
    </section>
  );
}
