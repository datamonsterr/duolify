import { useEffect, useState } from "react";
import { authRequest } from "../accounts/session";
import { trackLearningAnalyticsControl } from "../../lib/analytics";
import { useUiPreferences } from "../design/localization/Preferences";
import type { MessageId } from "../design/localization/messages";
import "./gamification.css";

interface CourseBreakdownItem {
  course_id: string;
  title: string;
  completed_lessons: number;
  total_xp: number;
  perfect_completions: number;
  total_duration_ms: number;
  last_completed_at: string;
}

interface DailyXPBreakdownItem {
  local_date: string;
  xp: number;
  lessons_completed: number;
  daily_goal_met: boolean;
}

interface StreakProtectionNotice {
  protected_on: string;
  remaining: number;
  streak_length: number;
}

interface StreakBreakdown {
  current_streak: number;
  longest_streak: number;
  freezes_held: number;
  freezes_used: number;
  recent_protections?: StreakProtectionNotice[];
}

interface CompletionBreakdown {
  total_completions: number;
  perfect_completions: number;
  total_duration_ms: number;
  average_duration_ms: number;
  first_completed_at?: string;
  last_completed_at?: string;
}

interface AnalyticsData {
  plan_id: string;
  plan_name: string;
  entitlement_version: string;
  time_zone: string;
  local_date: string;
  as_of: string;
  custom_export_allowed: boolean;
  summary: {
    total_xp: number;
    total_lessons_completed: number;
    perfect_completions: number;
    total_duration_ms: number;
    current_streak: number;
    longest_streak: number;
    freezes_held: number;
    freezes_used: number;
  };
  course_breakdown: CourseBreakdownItem[];
  daily_xp_breakdown: DailyXPBreakdownItem[];
  streak_breakdown: StreakBreakdown;
  completion_breakdown: CompletionBreakdown;
}

type DimensionKey = "course" | "daily" | "streak" | "completions" | "leagues";

// Canonical order mirrors the server allowlist; the server re-validates every request.
const dimensionKeys: DimensionKey[] = [
  "course",
  "daily",
  "streak",
  "completions",
  "leagues",
];

type Denial = { code: string; suppressUpsell: boolean };

function download(name: string, payload: unknown) {
  const blob = new Blob([JSON.stringify(payload, null, 2)], {
    type: "application/json",
  });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `${name}-${new Date().toISOString().slice(0, 10)}.json`;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 60000);
}

export function LearningAnalytics() {
  const { t, number, date } = useUiPreferences();
  const [data, setData] = useState<AnalyticsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<MessageId | null>(null);
  const [denial, setDenial] = useState<Denial | null>(null);
  const [revision, setRevision] = useState(0);
  const [busy, setBusy] = useState<"history" | "custom" | null>(null);
  const [notice, setNotice] = useState<MessageId | null>(null);
  const [selected, setSelected] = useState<Record<DimensionKey, boolean>>({
    course: true,
    daily: true,
    streak: false,
    completions: false,
    leagues: false,
  });

  // The app shell emits ui_screen_viewed(learning_analytics) once per route change.
  useEffect(() => {
    let active = true;
    authRequest<AnalyticsData>(
      "/v1/gamification/analytics",
      undefined,
      undefined,
      "GET",
    )
      .then((res) => {
        if (!active) return;
        setData(res);
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (!active) return;
        setLoading(false);
        const e = err as {
          status?: number;
          details?: { code?: string; upsell_suppressed?: boolean };
        };
        if (e?.status === 403 && e.details?.code) {
          setDenial({
            code: e.details.code,
            suppressUpsell: e.details.upsell_suppressed === true,
          });
        } else {
          setError(
            typeof navigator !== "undefined" && navigator.onLine === false
              ? "la.offline"
              : err instanceof TypeError
                ? "la.offline"
                : "la.error",
          );
        }
      });
    return () => {
      active = false;
    };
  }, [revision]);

  function retry() {
    setLoading(true);
    setError(null);
    setDenial(null);
    setRevision((r) => r + 1);
  }

  async function runExport(
    kind: "history" | "custom",
    path: string,
    body: unknown,
    method: string,
    file: string,
  ) {
    setBusy(kind);
    setNotice(null);
    trackLearningAnalyticsControl(
      kind === "history" ? "export_history" : "export_analytics",
    );
    try {
      download(file, await authRequest<unknown>(path, body, undefined, method));
      setNotice(kind === "history" ? "la.history_done" : "la.custom_done");
    } catch {
      setNotice(kind === "history" ? "la.history_failed" : "la.custom_failed");
    } finally {
      setBusy(null);
    }
  }

  const exportHistory = () =>
    runExport(
      "history",
      "/v1/gamification/export/history",
      undefined,
      "GET",
      "duolify-learning-history",
    );

  function exportCustom() {
    const dimensions = dimensionKeys.filter((k) => selected[k]);
    if (dimensions.length === 0) {
      setNotice("la.pick_one");
      return;
    }
    void runExport(
      "custom",
      "/v1/gamification/export/analytics",
      { dimensions },
      "POST",
      "duolify-analytics-export",
    );
  }

  const minutes = (ms: number) =>
    t("la.minutes", { count: Math.round(ms / 60000) });
  const when = (iso?: string) =>
    iso ? date(new Date(iso), data?.time_zone ?? "UTC") : "–";
  const historyButton = (
    <button
      type="button"
      className="analytics-button"
      onClick={() => void exportHistory()}
      disabled={busy !== null}
    >
      {busy === "history" ? t("la.exporting") : t("la.export_history")}
    </button>
  );

  return (
    <section
      className="analytics-section"
      aria-label={t("la.region")}
      aria-busy={loading}
    >
      <div className="analytics-hero">
        <div>
          <span className="analytics-badge">
            {data ? data.plan_name : t("la.title")}
          </span>
          <h1 className="analytics-title">{t("la.title")}</h1>
          {data && (
            <p className="analytics-meta">
              {t("la.timezone", {
                zone: data.time_zone,
                date: data.local_date,
              })}
            </p>
          )}
        </div>
      </div>

      {loading && <p role="status">{t("la.loading")}</p>}

      {denial?.code === "subscription_expired" && (
        <div className="analytics-upsell-card" role="alert">
          <h2>{t("la.expired_title")}</h2>
          <p>{t("la.expired_body")}</p>
          <a className="analytics-link" href="/credits">
            {t("la.renew")}
          </a>
        </div>
      )}

      {denial && denial.code !== "subscription_expired" && (
        <div className="analytics-upsell-card">
          <h2>
            {t(
              denial.suppressUpsell
                ? "la.not_included_title"
                : "la.required_title",
            )}
          </h2>
          <p>
            {t(
              denial.suppressUpsell
                ? "la.not_included_body"
                : "la.required_body",
            )}
          </p>
          <div className="analytics-actions">
            {denial.suppressUpsell ? (
              historyButton
            ) : (
              <a className="analytics-link" href="/credits">
                {t("la.view_plans")}
              </a>
            )}
          </div>
          {notice && denial.suppressUpsell && <p role="status">{t(notice)}</p>}
        </div>
      )}

      {error && (
        <div className="analytics-error" role="alert">
          <p>{t(error)}</p>
          <button type="button" className="analytics-button" onClick={retry}>
            {t("la.retry")}
          </button>
        </div>
      )}

      {data && (
        <>
          <div className="analytics-grid">
            <div className="analytics-card">
              <span>{t("la.total_xp")}</span>
              <strong>{number(data.summary.total_xp)}</strong>
            </div>
            <div className="analytics-card">
              <span>{t("la.completed")}</span>
              <strong>{number(data.summary.total_lessons_completed)}</strong>
            </div>
            <div className="analytics-card">
              <span>{t("la.perfect")}</span>
              <strong>{number(data.summary.perfect_completions)}</strong>
            </div>
            <div className="analytics-card">
              <span>{t("la.streak")}</span>
              <strong>{number(data.summary.current_streak)}</strong>
              <span>
                {t("la.longest", { count: data.summary.longest_streak })}
              </span>
            </div>
            <div className="analytics-card">
              <span>{t("la.freezes")}</span>
              <strong>{number(data.summary.freezes_held)}</strong>
              <span>{t("la.used", { count: data.summary.freezes_used })}</span>
            </div>
          </div>

          <div className="analytics-card">
            <h2>{t("la.courses")}</h2>
            {data.course_breakdown.length === 0 ? (
              <p>{t("la.courses_empty")}</p>
            ) : (
              <div className="analytics-scroll">
                <table className="analytics-table">
                  <thead>
                    <tr>
                      <th scope="col">{t("la.col_course")}</th>
                      <th scope="col">{t("la.col_lessons")}</th>
                      <th scope="col">{t("la.col_xp")}</th>
                      <th scope="col">{t("la.col_perfect")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.course_breakdown.map((c) => (
                      <tr key={c.course_id}>
                        <th scope="row" className="analytics-wrap">
                          {c.title}
                        </th>
                        <td>{number(c.completed_lessons)}</td>
                        <td>{t("la.xp", { count: c.total_xp })}</td>
                        <td>{number(c.perfect_completions)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <div className="analytics-card">
            <h2>{t("la.daily")}</h2>
            {data.daily_xp_breakdown.length === 0 ? (
              <p>{t("la.daily_empty")}</p>
            ) : (
              <div className="analytics-scroll">
                <table className="analytics-table">
                  <thead>
                    <tr>
                      <th scope="col">{t("la.col_date")}</th>
                      <th scope="col">{t("la.col_earned")}</th>
                      <th scope="col">{t("la.col_lessons")}</th>
                      <th scope="col">{t("la.col_goal")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.daily_xp_breakdown.map((d) => (
                      <tr key={d.local_date}>
                        <th scope="row">{d.local_date}</th>
                        <td>{t("la.xp", { count: d.xp })}</td>
                        <td>{number(d.lessons_completed)}</td>
                        <td>
                          {t(d.daily_goal_met ? "la.goal_met" : "la.goal_open")}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <div className="analytics-card">
            <h2>{t("la.completion")}</h2>
            <dl className="analytics-facts">
              <dt>{t("la.avg_time")}</dt>
              <dd>{minutes(data.completion_breakdown.average_duration_ms)}</dd>
              <dt>{t("la.total_time")}</dt>
              <dd>{minutes(data.completion_breakdown.total_duration_ms)}</dd>
              <dt>{t("la.first_done")}</dt>
              <dd>{when(data.completion_breakdown.first_completed_at)}</dd>
              <dt>{t("la.last_done")}</dt>
              <dd>{when(data.completion_breakdown.last_completed_at)}</dd>
            </dl>
            {(data.streak_breakdown.recent_protections ?? []).length > 0 && (
              <>
                <h3>{t("la.freeze_days")}</h3>
                <ul className="analytics-days">
                  {(data.streak_breakdown.recent_protections ?? []).map((p) => (
                    <li key={p.protected_on}>{p.protected_on}</li>
                  ))}
                </ul>
              </>
            )}
          </div>

          <div className="analytics-export-panel">
            <h2>{t("la.exports")}</h2>
            <p className="analytics-meta">{t("la.exports_note")}</p>
            <div className="analytics-actions">{historyButton}</div>

            <h3>{t("la.custom")}</h3>
            {data.custom_export_allowed ? (
              <fieldset className="analytics-fieldset">
                <legend>{t("la.dimensions")}</legend>
                <div className="analytics-dimension-list">
                  {dimensionKeys.map((k) => (
                    <label key={k} className="analytics-dimension-label">
                      <input
                        type="checkbox"
                        checked={selected[k]}
                        onChange={() =>
                          setSelected((prev) => ({ ...prev, [k]: !prev[k] }))
                        }
                      />
                      {t(`la.dim_${k}` as MessageId)}
                    </label>
                  ))}
                </div>
                <button
                  type="button"
                  className="analytics-button"
                  onClick={exportCustom}
                  disabled={busy !== null}
                >
                  {busy === "custom"
                    ? t("la.exporting")
                    : t("la.export_custom")}
                </button>
              </fieldset>
            ) : (
              <p className="analytics-meta">{t("la.custom_locked")}</p>
            )}
            {notice && <p role="status">{t(notice)}</p>}
          </div>
        </>
      )}
    </section>
  );
}

export function LearningAnalyticsPage() {
  return (
    <div className="xp-page">
      <LearningAnalytics />
    </div>
  );
}
