import { useCallback, useEffect, useRef, useState } from "react";
import {
  actionLabel,
  fetchNotices,
  fileAppeal,
  moderationFailure,
  noticeLabel,
  reasonCodes,
  type Notice,
} from "./moderation";
import { ReasonDialog } from "./ReasonDialog";
import "./sharing.css";
import "./market.css";
import "./moderation.css";

const appealMaxChars = 1000;
const appealWords = {
  open: "Appeal under review",
  upheld: "Appeal upheld",
  overturned: "Appeal overturned",
};
const headline = (n: Notice) => {
  const noun =
    n.target_type === "review"
      ? "review"
      : n.target_type === "user"
        ? "account"
        : "course";
  switch (n.action) {
    case "warn":
      return `Your ${noun} received a warning.`;
    case "delist":
      return n.target_type === "publication"
        ? "Your publication was not approved."
        : n.target_type === "review"
          ? "Your review was hidden."
          : "Your course was delisted from the Marketplace.";
    case "takedown":
      return n.target_type === "review"
        ? "Your review was removed."
        : "Your course was taken down.";
    case "ban_publishing":
      return "Publishing is turned off for your account.";
    default:
      return "A decision was made.";
  }
};

/** What the author sees: every action against their content, the reason and the one appeal. */
export function ModerationNotices() {
  const [state, setState] = useState<
    | { kind: "loading" }
    | { kind: "ready"; notices: Notice[] }
    | { kind: "failed"; error: unknown }
  >({ kind: "loading" });
  const [appealing, setAppealing] = useState<Notice>();
  const [sent, setSent] = useState("");
  const opener = useRef<HTMLElement | null>(null);

  const load = useCallback(() => {
    let active = true;
    fetchNotices().then(
      (notices) => active && setState({ kind: "ready", notices }),
      (error: unknown) => active && setState({ kind: "failed", error }),
    );
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => load(), [load]);

  return (
    <section className="mod" aria-busy={state.kind === "loading"}>
      <h1>Moderation notices</h1>
      {state.kind === "loading" && (
        <p role="status" aria-label="Loading notices">
          <span className="sharing-spinner" aria-hidden="true" />
          Loading notices…
        </p>
      )}
      {state.kind === "failed" && (
        <div className="sharing-error" role="alert">
          <p>{moderationFailure(state.error)}</p>
          <button
            type="button"
            className="duo-button"
            onClick={() => {
              setState({ kind: "loading" });
              load();
            }}
          >
            Retry
          </button>
        </div>
      )}
      {sent && (
        <p role="status" className="mod-done">
          {sent}
        </p>
      )}
      {state.kind === "ready" && state.notices.length === 0 && (
        <div className="mod-empty">
          <p>No moderation notices</p>
          <p className="sharing-note">
            Nothing has been decided about your courses or reviews.
          </p>
        </div>
      )}
      {state.kind === "ready" &&
        state.notices.map((n) => (
          <article
            key={n.id}
            className="mod-box mod-notice"
            aria-label={`Notice: ${noticeLabel(n)}`}
          >
            <h2 dir="auto">{n.title || "Your account"}</h2>
            <p>{headline(n)}</p>
            <blockquote dir="auto">{n.reason}</blockquote>
            <p className="sharing-note">
              {actionLabel(n.target_type, n.action)} ·{" "}
              {reasonCodes.find((c) => c.id === n.reason_code)?.label ??
                n.reason_code}
            </p>
            {n.severe && n.action === "takedown" && (
              <p className="sharing-note">
                Existing copies were blocked for learners who had this course.
              </p>
            )}
            {n.appeal && (
              <span
                className="mod-chip"
                data-tone={n.appeal.status === "open" ? "info" : "plain"}
              >
                {appealWords[n.appeal.status]}
              </span>
            )}
            {n.can_appeal && (
              <button
                type="button"
                className="duo-button secondary"
                onClick={(e) => {
                  opener.current = e.currentTarget;
                  setSent("");
                  setAppealing(n);
                }}
              >
                Appeal decision
              </button>
            )}
            {!n.can_appeal && !n.appeal && (
              <p className="sharing-note">This decision can't be appealed.</p>
            )}
          </article>
        ))}
      {appealing && (
        <ReasonDialog
          title="Appeal this decision"
          reasonLabel="Why should this be reviewed?"
          submitLabel="Send appeal"
          maxChars={appealMaxChars}
          help="You can appeal once. A different moderator will decide."
          returnFocus={opener}
          onClose={() => setAppealing(undefined)}
          onSubmit={async (_code, text) => {
            await fileAppeal(appealing.id, text);
            const id = appealing.id;
            setState((s) =>
              s.kind === "ready"
                ? {
                    kind: "ready",
                    notices: s.notices.map((x) =>
                      x.id === id
                        ? {
                            ...x,
                            can_appeal: false,
                            appeal: { status: "open" as const },
                          }
                        : x,
                    ),
                  }
                : s,
            );
            setSent("Appeal sent — another moderator will review it.");
            setAppealing(undefined);
          }}
        />
      )}
    </section>
  );
}
