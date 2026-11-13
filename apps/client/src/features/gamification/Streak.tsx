import { useEffect, useState } from "react";
import { authRequest } from "../accounts/session";
import { useUiPreferences } from "../design/localization/Preferences";
import "./gamification.css";

type DayState =
  "done" | "frozen" | "missed" | "today" | "pending" | "future" | "none";
interface StreakData {
  current_days: number;
  longest_days: number;
  today_done: boolean;
  local_date: string;
  time_zone: string;
  as_of: string;
  rollover_pending: boolean;
  month: string;
  calendar: { date: string; state: DayState }[];
  freezes?: {
    held: number;
    cap: number;
    milestone_days: number;
    next_milestone_in: number;
  };
  last_earned?: { reached_on: string; streak_length: number } | null;
  notices?: {
    protected_on: string;
    remaining: number;
    streak_length: number;
  }[];
}

const labels: Partial<Record<DayState, string>> = {
  done: "Done",
  frozen: "Freeze covered",
  missed: "Missed",
  today: "Today",
  pending: "Pending",
};
const utc = (date: string) => new Date(`${date}T00:00:00Z`);
const longDate = (date: string) =>
  new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeZone: "UTC",
  }).format(utc(date));
const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

function shiftMonth(month: string, delta: number) {
  const [year, mon] = month.split("-").map(Number);
  const moved = new Date(Date.UTC(year, mon - 1 + delta, 1));
  return `${moved.getUTCFullYear()}-${String(moved.getUTCMonth() + 1).padStart(2, "0")}`;
}

export function StreakPanel() {
  const { t } = useUiPreferences();
  const [data, setData] = useState<StreakData>();
  const [month, setMonth] = useState<string>();
  const [error, setError] = useState("");
  const [list, setList] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    const path = month
      ? `/v1/gamification/streak?month=${month}`
      : "/v1/gamification/streak";
    authRequest<StreakData>(path, undefined, undefined, "GET")
      .then((next) => {
        if (active) {
          setData(next);
          setError("");
        }
      })
      .catch((err: unknown) => {
        if (active)
          setError(
            err instanceof Error ? err.message : "Streak unavailable. Retry.",
          );
      });
    return () => {
      active = false;
    };
  }, [month, revision]);
  function go(delta: number) {
    if (data) setMonth(shiftMonth(data.month, delta));
  }
  const heading = data
    ? new Intl.DateTimeFormat("en-US", {
        month: "long",
        year: "numeric",
        timeZone: "UTC",
      }).format(utc(`${data.month}-01`))
    : "";
  const marked = data?.calendar.filter((d) => labels[d.state]) ?? [];
  // Monday-first grid with leading blanks.
  const lead = data
    ? (utc(data.calendar[0]?.date ?? `${data.month}-01`).getUTCDay() + 6) % 7
    : 0;
  const cells: (StreakData["calendar"][number] | null)[] = data
    ? [...Array<null>(lead).fill(null), ...data.calendar]
    : [];
  while (cells.length % 7) cells.push(null);
  const rows = Array.from({ length: cells.length / 7 }, (_, i) =>
    cells.slice(i * 7, i * 7 + 7),
  );
  return (
    <section className="streak-panel" aria-label="Your streak">
      {error && (
        <div role="alert">
          <p>{error}</p>
          <button type="button" onClick={() => setRevision((x) => x + 1)}>
            Retry streak
          </button>
        </div>
      )}
      {!data && !error && <p role="status">Loading streak…</p>}
      {data && (
        <>
          <h2>Your streak</h2>
          <div className="streak-hero">
            <strong className="streak-count">{data.current_days}</strong>
            <span>day streak</span>
          </div>
          <p>Longest streak: {data.longest_days} days</p>
          <p>Time zone: {data.time_zone}</p>
          {data.rollover_pending && (
            <div className="streak-note" role="status">
              <strong>Rollover in progress</strong>
              <p>Updating your streak for the new day…</p>
              <p>
                Streak as of{" "}
                {new Date(data.as_of).toLocaleString(undefined, {
                  dateStyle: "medium",
                  timeStyle: "short",
                })}
              </p>
            </div>
          )}
          {data.current_days === 0 && data.longest_days > 0 && (
            <div className="streak-note">
              <strong>Welcome back</strong>
              <p>
                Start with one small lesson. Your longest streak (
                {data.longest_days} days) is still on record.
              </p>
              <a href="/">Start a lesson</a>
            </div>
          )}
          {data.current_days === 0 && data.longest_days === 0 && (
            <div className="streak-note">
              <strong>Start your streak</strong>
              <p>Complete one lesson to begin. Days follow your time zone.</p>
              <a href="/">Start a lesson</a>
            </div>
          )}
          {data.current_days > 0 && data.today_done && (
            <p className="streak-note">
              <strong>Today is done</strong>
            </p>
          )}
          {data.current_days > 0 &&
            !data.today_done &&
            !data.rollover_pending && (
              <div className="streak-note">
                <strong>Today isn't done yet</strong>
                <p>
                  Finish one lesson before the day ends to make it{" "}
                  {data.current_days + 1}.
                </p>
                <a href="/">Start a lesson</a>
              </div>
            )}
          {data.freezes && (
            <div className="streak-freezes">
              <p>
                <strong>
                  Streak freezes: {data.freezes.held} of {data.freezes.cap}
                </strong>
              </p>
              <p>Earned every {data.freezes.milestone_days} days of streak</p>
              {data.freezes.held >= data.freezes.cap ? (
                <p>Freezes are full. Extra milestones earn nothing.</p>
              ) : (
                <p>
                  Next freeze in {plural(data.freezes.next_milestone_in, "day")}
                </p>
              )}
              {data.freezes.held > 0 && (
                <p>
                  {plural(data.freezes.held, "freeze")}{" "}
                  {data.freezes.held === 1 ? "applies" : "apply"} automatically
                  on a missed day.
                </p>
              )}
              {data.last_earned && (
                <p>
                  Freeze earned on {longDate(data.last_earned.reached_on)} at a{" "}
                  {data.last_earned.streak_length}-day streak
                </p>
              )}
            </div>
          )}
          {data.notices?.map((n) => (
            <div className="streak-note" role="status" key={n.protected_on}>
              <strong>A freeze covered {longDate(n.protected_on)}</strong>
              <p>
                Your streak stayed at {n.streak_length}.{" "}
                {n.remaining === 0
                  ? "No freezes left."
                  : `${plural(n.remaining, "freeze")} left.`}
              </p>
            </div>
          ))}
          <a href="/analytics">{t("la.open")}</a>
          <div className="streak-toolbar">
            <button type="button" onClick={() => go(-1)}>
              Previous month
            </button>
            <h3>{heading}</h3>
            <button type="button" onClick={() => go(1)}>
              Next month
            </button>
          </div>
          <button type="button" onClick={() => setList((x) => !x)}>
            {list ? "Show as calendar" : "Show as list"}
          </button>
          {list ? (
            <ul className="streak-list" aria-label={heading}>
              {marked.map((day) => (
                <li key={day.date}>
                  {longDate(day.date)}: {labels[day.state]}
                </li>
              ))}
              {marked.length === 0 && <li>No marked days this month.</li>}
            </ul>
          ) : (
            <table className="streak-calendar" aria-label={heading}>
              <thead>
                <tr>
                  {["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"].map((d) => (
                    <th key={d} scope="col">
                      {d}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row, i) => (
                  <tr key={i}>
                    {row.map((day, j) =>
                      day ? (
                        <td key={day.date} data-state={day.state}>
                          {Number(day.date.slice(8))}
                          {labels[day.state] && (
                            <>
                              {" "}
                              <span>{labels[day.state]}</span>
                            </>
                          )}
                        </td>
                      ) : (
                        <td key={j} aria-hidden="true" />
                      ),
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </section>
  );
}
