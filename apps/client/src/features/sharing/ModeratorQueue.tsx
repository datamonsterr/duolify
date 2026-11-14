import { useCallback, useEffect, useRef, useState } from "react";
import {
  actionLabel,
  actionsFor,
  auditWords,
  decideAppeal,
  defaultTargetHours,
  dialogTitle,
  fetchQueue,
  isUnavailable,
  moderationFailure,
  reasonCodes,
  reasonMaxChars,
  sendAction,
  severityLabel,
  targetLabels,
  type AppealItem,
  type ModerationAction,
  type QueueItem,
  type QueueView,
} from "./moderation";
import { ReasonDialog } from "./ReasonDialog";
import "./sharing.css";
import "./market.css";
import "./moderation.css";

type State =
  | { kind: "loading" }
  | { kind: "ready"; view: QueueView }
  | { kind: "unavailable" }
  | { kind: "failed"; error: unknown };
type Filter = "all" | "course" | "review" | "user" | "screening";
const filters: { id: Filter; label: string }[] = [
  { id: "all", label: "All" },
  { id: "course", label: "Courses" },
  { id: "review", label: "Reviews" },
  { id: "user", label: "Users" },
  { id: "screening", label: "Screening" },
];
type Dialog =
  | { kind: "action"; item: QueueItem; action: ModerationAction }
  | { kind: "appeal"; appeal: AppealItem; decision: "uphold" | "overturn" };

const age = (iso: string) => {
  const hours = Math.max(
    0,
    Math.floor((Date.now() - new Date(iso).getTime()) / 36e5),
  );
  return hours >= 48 ? `${Math.floor(hours / 24)} d` : `${hours} h`;
};
const when = (iso: string) =>
  new Date(iso).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
const reasonLabel = (item: QueueItem) =>
  item.screening
    ? `Screening: ${item.screening.verdict.replace("_", " ")}`
    : item.reasons.join(", ") || "Reported";
const rowStatus = (item: QueueItem) =>
  item.screening
    ? item.screening.verdict === "needs_review"
      ? "Needs review"
      : "Pending screening"
    : item.hidden
      ? "Auto-hidden"
      : "Open";
const matches = (item: QueueItem, f: Filter) =>
  f === "all" ||
  (f === "screening"
    ? item.target_type === "publication"
    : item.target_type === f);

/** The moderator surface: queue by severity then age, report detail, audited action dialog, open appeals. */
export function ModeratorQueue() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [filter, setFilter] = useState<Filter>("all");
  const [selected, setSelected] = useState<string>();
  const [dialog, setDialog] = useState<Dialog>();
  const [done, setDone] = useState("");
  const opener = useRef<HTMLElement | null>(null);

  const load = useCallback(() => {
    let active = true;
    fetchQueue().then(
      (view) => active && setState({ kind: "ready", view }),
      (error: unknown) =>
        active &&
        setState(
          isUnavailable(error)
            ? { kind: "unavailable" }
            : { kind: "failed", error },
        ),
    );
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => load(), [load]);
  const reload = () => {
    setState({ kind: "loading" });
    load();
  };

  if (state.kind === "unavailable")
    return (
      <section className="mod">
        <h1 tabIndex={-1}>This page isn't available</h1>
        <p>Try your courses or return to your learning path.</p>
        <a className="duo-button secondary" href="/courses">
          Back to Courses
        </a>
      </section>
    );
  if (state.kind === "loading")
    return (
      <section className="mod" aria-busy="true">
        <h1>Moderation</h1>
        <p role="status" aria-label="Loading queue">
          <span className="sharing-spinner" aria-hidden="true" />
          Loading queue…
        </p>
      </section>
    );
  if (state.kind === "failed")
    return (
      <section className="mod">
        <h1>Moderation</h1>
        <div className="sharing-error" role="alert">
          <p>{moderationFailure(state.error)}</p>
          <button type="button" className="duo-button" onClick={reload}>
            Retry
          </button>
        </div>
      </section>
    );

  const { view } = state;
  const items = view.items.filter((i) => matches(i, filter));
  const current = view.items.find((i) => i.id === selected);
  const target = view.items[0]
    ? Math.round(
        (new Date(view.items[0].due_at).getTime() -
          new Date(view.items[0].opened_at).getTime()) /
          36e5,
      )
    : defaultTargetHours;
  const mutate = (fn: (v: QueueView) => QueueView) =>
    setState((s) =>
      s.kind === "ready" ? { kind: "ready", view: fn(s.view) } : s,
    );
  const openAction = (
    item: QueueItem,
    action: ModerationAction,
    el: HTMLElement,
  ) => {
    opener.current = el;
    setDialog({ kind: "action", item, action });
  };

  return (
    <section className="mod">
      <header className="mod-head">
        <h1>Moderation</h1>
        <span className="mod-chip" data-tone="bad">
          {view.open_count} open
        </span>
        <span className="mod-chip" data-tone="warn">
          {view.overdue_count} over {target || defaultTargetHours} h
        </span>
        <span className="mod-chip" data-tone="info">
          {view.screening_count} awaiting screening review
        </span>
        <button type="button" className="duo-button secondary" onClick={reload}>
          Refresh queue
        </button>
      </header>
      {done && (
        <p role="status" className="mod-done">
          {done}
        </p>
      )}
      <div className="mod-layout" data-screen={current ? "detail" : "list"}>
        <section className="mod-list" aria-label="Report queue">
          <div className="mod-filters" role="group" aria-label="Filter queue">
            {filters.map((f) => (
              <button
                key={f.id}
                type="button"
                className="mod-filter"
                aria-pressed={filter === f.id}
                onClick={() => setFilter(f.id)}
              >
                {f.label}
              </button>
            ))}
          </div>
          {items.length === 0 ? (
            <div className="mod-empty">
              <p>Nothing is waiting for review</p>
            </div>
          ) : (
            <ul>
              {items.map((item) => (
                <li key={item.id}>
                  <button
                    type="button"
                    className="mod-row"
                    aria-label={`${targetLabels[item.target_type]} · ${item.title}`}
                    aria-current={item.id === selected}
                    onClick={() => {
                      setDone("");
                      setSelected(item.id);
                    }}
                  >
                    <span
                      className="mod-chip"
                      data-tone={
                        severityLabel(item.severity) === "High"
                          ? "bad"
                          : severityLabel(item.severity) === "Medium"
                            ? "warn"
                            : "plain"
                      }
                    >
                      {severityLabel(item.severity)}
                    </span>
                    <span className="mod-row-main">
                      <b dir="auto">
                        {targetLabels[item.target_type]} · {item.title}
                      </b>
                      <span className="sharing-note">
                        {reasonLabel(item)} ·{" "}
                        {item.report_count > 0
                          ? `${item.report_count} reports · `
                          : ""}
                        {age(item.opened_at)}
                      </span>
                    </span>
                    <span
                      className="mod-chip"
                      data-tone={item.overdue ? "warn" : "plain"}
                    >
                      {item.overdue ? "Overdue" : rowStatus(item)}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>
        {current && (
          <section className="mod-detail" aria-label="Report detail">
            <button
              type="button"
              className="mod-back duo-button secondary"
              onClick={() => setSelected(undefined)}
            >
              Back to queue
            </button>
            <h2 dir="auto">{current.title}</h2>
            <div className="mod-chips">
              {current.report_count > 0 && (
                <span className="mod-chip" data-tone="bad">
                  {current.report_count}{" "}
                  {current.report_count === 1 ? "reporter" : "reporters"} ·{" "}
                  {current.reasons.join(", ")}
                </span>
              )}
              {current.hidden && (
                <span className="mod-chip" data-tone="warn">
                  Auto-hidden from listings
                </span>
              )}
              {current.overdue && (
                <span className="mod-chip" data-tone="plain">
                  Past {target || defaultTargetHours} h target
                </span>
              )}
            </div>
            {current.screening && (
              <div className="mod-box">
                <b>Screening: {current.screening.verdict.replace("_", " ")}</b>
                <span className="sharing-note">
                  Version {current.version} · categories:{" "}
                  {current.screening.codes.join(", ") || "none"} · sampled{" "}
                  {current.screening.sampled_lessons} lessons.
                </span>
              </div>
            )}
            <div className="mod-actions">
              {actionsFor(current.target_type).map((action) => (
                <button
                  key={action}
                  type="button"
                  className={`duo-button ${action === "takedown" ? "" : "secondary"}`}
                  onClick={(e) => openAction(current, action, e.currentTarget)}
                >
                  {actionLabel(current.target_type, action)}
                </button>
              ))}
            </div>
            <b>Audit log</b>
            {current.audit.length === 0 ? (
              <p className="sharing-note">No earlier actions on this item.</p>
            ) : (
              <ul className="mod-log">
                {current.audit.map((a, i) => (
                  <li key={i}>
                    <span className="sharing-note">{when(a.at)}</span>
                    <span>
                      {auditWords[a.action] ?? a.action} ({a.reason_code})
                    </span>
                  </li>
                ))}
              </ul>
            )}
            <p className="sharing-note">
              The owner is notified with the reason and can appeal once.
            </p>
          </section>
        )}
      </div>
      {view.appeals.length > 0 && (
        <section className="mod-appeals" aria-label="Open appeals">
          <h2>Open appeals</h2>
          {view.appeals.map((a) => (
            <article
              key={a.id}
              className="mod-box"
              aria-label={`Appeal: ${a.title}`}
            >
              <b dir="auto">{a.title}</b>
              <span className="sharing-note">
                {auditWords[a.action] ?? a.action} ({a.reason_code}) ·{" "}
                {when(a.created_at)}
              </span>
              <p dir="auto">{a.text}</p>
              <div className="mod-buttons">
                <button
                  type="button"
                  className="duo-button secondary"
                  onClick={(e) => {
                    opener.current = e.currentTarget;
                    setDialog({
                      kind: "appeal",
                      appeal: a,
                      decision: "uphold",
                    });
                  }}
                >
                  Uphold
                </button>
                <button
                  type="button"
                  className="duo-button"
                  onClick={(e) => {
                    opener.current = e.currentTarget;
                    setDialog({
                      kind: "appeal",
                      appeal: a,
                      decision: "overturn",
                    });
                  }}
                >
                  Overturn
                </button>
              </div>
            </article>
          ))}
        </section>
      )}
      {dialog?.kind === "action" && (
        <ReasonDialog
          title={dialogTitle(dialog.item.target_type, dialog.action)}
          reasonLabel="Reason sent to owner"
          submitLabel="Confirm action"
          maxChars={reasonMaxChars}
          help="The owner sees this reason. It is kept in the audit log."
          codes={reasonCodes}
          initialCode={
            dialog.action === "dismiss"
              ? "no_violation"
              : (dialog.item.reasons[0] ?? "other")
          }
          action={dialog.action}
          returnFocus={opener}
          onClose={() => setDialog(undefined)}
          onSubmit={async (code, reason) => {
            await sendAction(dialog.item.id, dialog.action, code, reason);
            const id = dialog.item.id;
            mutate((v) => ({
              ...v,
              items: v.items.filter((i) => i.id !== id),
              open_count: Math.max(0, v.open_count - 1),
              overdue_count: Math.max(
                0,
                v.overdue_count - (dialog.item.overdue ? 1 : 0),
              ),
              screening_count: Math.max(
                0,
                v.screening_count -
                  (dialog.item.target_type === "publication" ? 1 : 0),
              ),
            }));
            setSelected(undefined);
            setDone("Action recorded");
            setDialog(undefined);
          }}
        />
      )}
      {dialog?.kind === "appeal" && (
        <ReasonDialog
          title={
            dialog.decision === "overturn"
              ? "Overturn appeal"
              : "Uphold decision"
          }
          reasonLabel="Reason sent to owner"
          submitLabel="Confirm action"
          maxChars={reasonMaxChars}
          help="A different moderator than the one who acted must decide."
          returnFocus={opener}
          onClose={() => setDialog(undefined)}
          onSubmit={async (_code, reason) => {
            await decideAppeal(dialog.appeal.id, dialog.decision, reason);
            const id = dialog.appeal.id;
            mutate((v) => ({
              ...v,
              appeals: v.appeals.filter((a) => a.id !== id),
            }));
            setDone("Appeal decided");
            setDialog(undefined);
          }}
        />
      )}
    </section>
  );
}
