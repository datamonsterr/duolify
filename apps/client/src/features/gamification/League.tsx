import { useEffect, useState } from "react";
import { authRequest } from "../accounts/session";
import { readLeagueCache, writeLeagueCache } from "./league-cache";
import "./gamification.css";

interface LeagueRow {
  rank: number;
  display_name: string;
  weekly_xp: number;
  tie: boolean;
  self: boolean;
  zone: string;
}
interface LastWeek {
  state: "final" | "closing" | "none";
  week: { start: string; end: string };
  league?: string;
  rank?: number;
  members?: number;
  weekly_xp?: number;
  outcome?: "promoted" | "demoted" | "stayed";
  next_league?: string;
}
interface LeagueData {
  last_week?: LastWeek;
  state: "active" | "not_joined";
  week: { start: string; end: string; days_left: number };
  as_of: string;
  local_date: string;
  time_zone: string;
  league?: {
    name: string;
    tier: number;
    members: number;
    promotion_top: number;
    demotion_from: number;
  };
  self?: { rank: number; weekly_xp: number; zone: string };
  rows: LeagueRow[];
  page: {
    offset: number;
    limit: number;
    total: number;
    next_offset: number | null;
    prev_offset: number | null;
  };
}

const utc = (date: string) => new Date(`${date}T00:00:00Z`);
const endLabel = (date: string) =>
  new Intl.DateTimeFormat("en-US", {
    weekday: "short",
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(utc(date));
const clock = (iso: string, zone: string) => {
  try {
    return new Intl.DateTimeFormat("en-GB", {
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
      timeZone: zone,
    }).format(new Date(iso));
  } catch {
    return new Date(iso).toISOString().slice(11, 16);
  }
};
const shortDay = (date: string) =>
  new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(utc(date));
const outcomeText = {
  promoted: (next: string) => `Promoted to ${next} league`,
  demoted: (next: string) => `Demoted to ${next} league`,
  stayed: (next: string) => `Stayed in ${next} league`,
};
const span = (from: number, to: number) =>
  from === to ? `rank ${from}` : `ranks ${from}–${to}`;
const zoneText: Record<string, string> = {
  promotion: " · promotion zone",
  demotion: " · demotion zone",
};

function LastWeekResult({ last }: { last?: LastWeek }) {
  if (last?.state === "none")
    return (
      <div className="streak-note" role="status">
        <p>You weren't in a league last week.</p>
      </div>
    );
  if (last?.state === "final" && last.outcome && last.rank) {
    return (
      <div className="streak-note" role="status">
        <h2>
          {outcomeText[last.outcome](last.next_league ?? last.league ?? "")}
        </h2>
        <p>
          {shortDay(last.week.start)} – {shortDay(last.week.end)}
        </p>
        <p>
          Last week: rank {last.rank} of {last.members} · {last.weekly_xp} XP in{" "}
          {last.league} league
        </p>
        <p>Results are final. Your streak doesn't depend on leagues.</p>
      </div>
    );
  }
  return (
    <div className="streak-note" role="status">
      <strong>Week is closing</strong>
      <p>Final ranks appear after every time zone finishes the day.</p>
    </div>
  );
}

export function LeaguePanel() {
  const [data, setData] = useState<LeagueData>();
  const [offset, setOffset] = useState(0);
  const [error, setError] = useState("");
  const [offline, setOffline] = useState(false);
  const [lastWeek, setLastWeek] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    const path =
      offset > 0
        ? `/v1/gamification/league?offset=${offset}`
        : "/v1/gamification/league";
    authRequest<LeagueData>(path, undefined, undefined, "GET")
      .then((next) => {
        if (!active) return;
        setData(next);
        setError("");
        setOffline(false);
        if (offset === 0) writeLeagueCache(next);
      })
      .catch((err: unknown) => {
        if (!active) return;
        const cached = readLeagueCache<LeagueData>();
        if (cached && offset === 0) {
          setData(cached);
          setOffline(true);
          setError("");
          return;
        }
        setError(
          err instanceof Error && err.message
            ? err.message
            : "League unavailable. Retry.",
        );
      });
    return () => {
      active = false;
    };
  }, [offset, revision]);

  const info = data?.league;
  const retry = () => {
    setError("");
    setRevision((x) => x + 1);
  };
  return (
    <section className="streak-panel league-panel" aria-label="Weekly league">
      {error && (
        <div role="alert">
          <p>{error}</p>
          <button type="button" onClick={retry}>
            Retry leagues
          </button>
        </div>
      )}
      {!data && !error && <p role="status">Loading league…</p>}
      {data && (
        <>
          <div className="streak-toolbar" role="group" aria-label="Week">
            <button
              type="button"
              aria-pressed={!lastWeek}
              onClick={() => setLastWeek(false)}
            >
              This week
            </button>
            <button
              type="button"
              aria-pressed={lastWeek}
              onClick={() => setLastWeek(true)}
            >
              Last week
            </button>
          </div>
          {offline && (
            <div className="streak-note" role="status">
              <p>
                Ranks as of {clock(data.as_of, data.time_zone)}. They'll refresh
                when you reconnect.
              </p>
              <button type="button" onClick={retry}>
                Retry leagues
              </button>
            </div>
          )}
          {lastWeek && <LastWeekResult last={data.last_week} />}
          {!lastWeek && data.state === "not_joined" && (
            <div className="streak-note">
              <h2>Join this week's league</h2>
              <p>Complete a lesson to join</p>
              <p>
                A group of learners at your pace is waiting. It's optional. Your
                streak doesn't depend on it.
              </p>
              <a href="/">Start a lesson</a>
            </div>
          )}
          {!lastWeek && data.state === "active" && info && (
            <>
              <h2>{info.name} league</h2>
              <p>
                Ends {endLabel(data.week.end)} · {data.week.days_left}{" "}
                {data.week.days_left === 1 ? "day" : "days"} left
              </p>
              <p>
                Every completed lesson adds to your week.
                {info.promotion_top > 0 &&
                  ` Top ${info.promotion_top} move up.`}
              </p>
              {data.self && (
                <p>
                  Your rank: {data.self.rank} · {data.self.weekly_xp} XP
                  {zoneText[data.self.zone] ?? ""}
                </p>
              )}
              <table className="league-table">
                <caption className="sr-only">Weekly XP ranking</caption>
                <thead>
                  <tr>
                    <th scope="col">Rank</th>
                    <th scope="col">Learner</th>
                    <th scope="col">Weekly XP</th>
                  </tr>
                </thead>
                <tbody>
                  {data.rows.flatMap((row) => {
                    const out = [];
                    if (
                      info.demotion_from > 0 &&
                      row.rank === info.demotion_from
                    )
                      out.push(
                        <tr key="demotion" className="league-zone">
                          <td colSpan={3}>
                            Demotion zone below (
                            {span(info.demotion_from, info.members)})
                          </td>
                        </tr>,
                      );
                    out.push(
                      <tr
                        key={row.rank}
                        aria-current={row.self ? "true" : undefined}
                        className={row.self ? "league-self" : undefined}
                      >
                        <td>{row.rank}</td>
                        <td>{row.self ? "You" : row.display_name}</td>
                        <td>
                          {row.weekly_xp} XP{row.tie ? " · tie" : ""}
                        </td>
                      </tr>,
                    );
                    if (
                      info.promotion_top > 0 &&
                      row.rank === info.promotion_top
                    )
                      out.push(
                        <tr key="promotion" className="league-zone">
                          <td colSpan={3}>
                            Promotion zone above ({span(1, info.promotion_top)})
                          </td>
                        </tr>,
                      );
                    return out;
                  })}
                </tbody>
              </table>
              <div className="streak-toolbar">
                <button
                  type="button"
                  disabled={data.page.prev_offset === null}
                  onClick={() => setOffset(data.page.prev_offset ?? 0)}
                >
                  Previous ranks
                </button>
                <p>
                  Showing ranks {data.page.offset + 1}–
                  {data.page.offset + data.rows.length} of {data.page.total}
                </p>
                <button
                  type="button"
                  disabled={data.page.next_offset === null}
                  onClick={() => setOffset(data.page.next_offset ?? 0)}
                >
                  Next ranks
                </button>
              </div>
            </>
          )}
        </>
      )}
    </section>
  );
}

export function LeaguePage() {
  return (
    <div className="xp-page">
      <h1>Leagues</h1>
      <LeaguePanel />
    </div>
  );
}
