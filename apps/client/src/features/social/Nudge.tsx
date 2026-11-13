import { nudgeNote, nudgeState, type NudgeEntry } from "./nudges";
import type { FriendEntry } from "./friends";
import "./social.css";

function Bell() {
  return (
    <svg
      className="nudge-icon"
      viewBox="0 0 24 24"
      width="18"
      height="18"
      aria-hidden="true"
      focusable="false"
    >
      <path
        fill="currentColor"
        d="M12 22a2.5 2.5 0 0 0 2.45-2h-4.9A2.5 2.5 0 0 0 12 22Zm7-6v-5a7 7 0 0 0-5.5-6.84V3.5a1.5 1.5 0 0 0-3 0v.66A7 7 0 0 0 5 11v5l-2 2v1h18v-1l-2-2Z"
      />
    </svg>
  );
}

/** Nudge button, or the reason it is not offered, for a friend row or detail. */
export function NudgeControl({
  entry,
  busy,
  onNudge,
}: {
  entry: FriendEntry;
  busy: boolean;
  onNudge: () => void;
}) {
  const state = nudgeState(entry);
  if (state !== "available")
    return (
      <p className="nudge-note" role="status" dir="auto">
        {nudgeNote(state, entry.display_name)}
      </p>
    );
  return (
    <button
      type="button"
      className="duo-button secondary nudge-button"
      disabled={busy}
      onClick={onNudge}
    >
      <Bell />
      Nudge <bdi>{entry.display_name}</bdi>
    </button>
  );
}

function when(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      });
}

/** In-app inbox of received nudges: who and when, nothing else. */
export function NudgeInbox({ items }: { items: NudgeEntry[] }) {
  if (items.length === 0)
    return <p className="social-hint">No nudges right now.</p>;
  return (
    <ul aria-label="Nudges" className="social-result-list">
      {items.map((item) => (
        <li key={item.id}>
          <span className="social-avatar small" aria-hidden="true">
            {item.avatar.initial}
          </span>
          <span className="social-card-text">
            <strong>
              <bdi>{item.display_name}</bdi> nudged you
            </strong>
            <span>
              @{item.username}
              {when(item.created_at) && ` · ${when(item.created_at)}`}
            </span>
          </span>
          {!item.read && <span className="social-chip">New</span>}
        </li>
      ))}
    </ul>
  );
}
