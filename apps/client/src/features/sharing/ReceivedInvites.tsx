import { useState } from "react";
import {
  gone,
  inviteFailure,
  inviteMessages,
  invites,
  type ReceivedInvite,
} from "./invites";
import "./sharing.css";

const when = (iso: string) => {
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
};

/** Course invites inbox: sender and course title only. Accept opens the new copy. */
export function ReceivedInvites({
  items,
  onResolved,
  onAccepted,
}: {
  items: ReceivedInvite[];
  onResolved: (id: string) => void;
  onAccepted: (courseId: string) => void;
}) {
  const [busy, setBusy] = useState("");
  const [problem, setProblem] = useState("");

  async function act(item: ReceivedInvite, kind: "accept" | "decline") {
    if (busy) return;
    setBusy(item.id);
    setProblem("");
    try {
      if (kind === "accept") {
        const result = await invites.accept(item.id);
        onResolved(item.id);
        onAccepted(result.course_id);
      } else {
        await invites.decline(item.id);
        onResolved(item.id);
      }
    } catch (error) {
      const failure = inviteFailure(error);
      if (failure === "unavailable") {
        setProblem(gone);
        onResolved(item.id);
      } else setProblem(inviteMessages.offline);
    } finally {
      setBusy("");
    }
  }

  return (
    <>
      {problem && (
        <p className="social-error" role="alert">
          {problem}
        </p>
      )}
      {items.length === 0 ? (
        <p className="social-hint">No course invites right now.</p>
      ) : (
        <ul aria-label="Course invites" className="social-result-list invites">
          {items.map((item) => (
            <li
              key={item.id}
              data-testid={`received-invite-${item.sender.username}-${item.course.title}`}
            >
              <span className="social-avatar small" aria-hidden="true">
                {item.sender.avatar.initial}
              </span>
              <span className="social-card-text">
                <strong>
                  <bdi>{item.sender.display_name}</bdi> invited you to{" "}
                  <bdi className="invite-title">{item.course.title}</bdi>
                </strong>
                <span>
                  @{item.sender.username}
                  {when(item.created_at) && ` · ${when(item.created_at)}`}
                  {!item.read && " · "}
                  {!item.read && <span className="social-chip">New</span>}
                </span>
              </span>
              <span className="invite-actions">
                <button
                  type="button"
                  className="duo-button"
                  aria-label="Accept"
                  disabled={busy !== ""}
                  onClick={() => void act(item, "accept")}
                >
                  Accept
                </button>
                <button
                  type="button"
                  className="duo-button secondary"
                  aria-label="Decline"
                  disabled={busy !== ""}
                  onClick={() => void act(item, "decline")}
                >
                  Decline
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
