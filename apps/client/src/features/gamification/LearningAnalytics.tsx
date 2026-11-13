import { useEffect, useState } from "react";
import { authRequest } from "../accounts/session";
import {
  trackScreen,
  trackLearningAnalyticsControl,
} from "../../lib/analytics";
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

const dimensionOptions: { id: DimensionKey; label: string }[] = [
  { id: "course", label: "Courses" },
  { id: "daily", label: "Daily XP" },
  { id: "streak", label: "Streaks" },
  { id: "completions", label: "Completions" },
  { id: "leagues", label: "Leagues" },
];

export function LearningAnalytics() {
  const [data, setData] = useState<AnalyticsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [errorCode, setErrorCode] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  const [exportingHistory, setExportingHistory] = useState(false);
  const [exportingAnalytics, setExportingAnalytics] = useState(false);
  const [exportNotice, setExportNotice] = useState<string | null>(null);
  const [selectedDimensions, setSelectedDimensions] = useState<
    Record<DimensionKey, boolean>
  >({
    course: true,
    daily: true,
    streak: false,
    completions: false,
    leagues: false,
  });

  useEffect(() => {
    trackScreen("learning_analytics");
  }, []);

  function handleRetry() {
    setLoading(true);
    setError(null);
    setErrorCode(null);
    setRevision((r) => r + 1);
  }

  useEffect(() => {
    let active = true;

    authRequest<AnalyticsData>(
      "/v1/gamification/analytics",
      undefined,
      undefined,
      "GET",
    )
      .then((res) => {
        if (active) {
          setData(res);
          setLoading(false);
        }
      })
      .catch((err: unknown) => {
        if (!active) return;
        setLoading(false);
        if (err && typeof err === "object") {
          const apiErr = err as {
            status?: number;
            message?: string;
            details?: { code?: string };
          };
          if (apiErr.status === 403 || apiErr.details?.code) {
            setErrorCode(apiErr.details?.code ?? "subscription_required");
            setError(
              apiErr.message ??
                "Plus or Pro subscription required for advanced learning analytics.",
            );
            return;
          }
        }
        setError(
          err instanceof Error
            ? err.message
            : "Learning analytics unavailable. Retry.",
        );
      });

    return () => {
      active = false;
    };
  }, [revision]);

  async function handleExportHistory() {
    setExportingHistory(true);
    setExportNotice(null);
    try {
      trackLearningAnalyticsControl("export_history");
      const exportData = await authRequest<Record<string, unknown>>(
        "/v1/gamification/export/history",
        undefined,
        undefined,
        "GET",
      );
      const blob = new Blob([JSON.stringify(exportData, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `duolify-learning-history-${new Date().toISOString().slice(0, 10)}.json`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 60000);
      setExportNotice("Learning history export downloaded.");
    } catch (err: unknown) {
      setExportNotice(
        err instanceof Error
          ? err.message
          : "Could not export learning history. Retry.",
      );
    } finally {
      setExportingHistory(false);
    }
  }

  async function handleExportCustomAnalytics() {
    const dimensions = (
      Object.keys(selectedDimensions) as DimensionKey[]
    ).filter((k) => selectedDimensions[k]);
    if (dimensions.length === 0) {
      setExportNotice("Select at least one dimension to export.");
      return;
    }
    setExportingAnalytics(true);
    setExportNotice(null);
    try {
      trackLearningAnalyticsControl("export_analytics");
      const exportData = await authRequest<Record<string, unknown>>(
        "/v1/gamification/export/analytics",
        { dimensions },
        undefined,
        "POST",
      );
      const blob = new Blob([JSON.stringify(exportData, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `duolify-analytics-export-${new Date().toISOString().slice(0, 10)}.json`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 60000);
      setExportNotice("Custom analytics export downloaded.");
    } catch (err: unknown) {
      setExportNotice(
        err instanceof Error
          ? err.message
          : "Could not export custom analytics. Retry.",
      );
    } finally {
      setExportingAnalytics(false);
    }
  }

  const toggleDimension = (dim: DimensionKey) => {
    setSelectedDimensions((prev) => ({ ...prev, [dim]: !prev[dim] }));
  };

  return (
    <section
      className="analytics-section"
      aria-label="Learning analytics"
      aria-busy={loading}
    >
      <div className="analytics-hero">
        <div>
          <span className="analytics-badge">
            {data ? data.plan_name : "Learning analytics"}
          </span>
          <h1 style={{ margin: "0.25rem 0 0", fontSize: "1.75rem" }}>
            Learning analytics
          </h1>
          {data && (
            <p style={{ margin: "0.25rem 0 0", fontSize: "0.875rem" }}>
              Time zone: {data.time_zone} · Local date: {data.local_date}
            </p>
          )}
        </div>
      </div>

      {loading && (
        <p role="status" style={{ padding: "1rem" }}>
          Loading learning analytics…
        </p>
      )}

      {error && errorCode === "subscription_expired" && (
        <div className="analytics-upsell-card" role="alert">
          <h2 style={{ margin: 0, fontSize: "1.25rem" }}>
            Subscription expired
          </h2>
          <p style={{ margin: 0 }}>
            Subscription expired. Renew to access learning analytics.
          </p>
          <a
            href="/credits"
            style={{
              display: "inline-block",
              fontWeight: 800,
              color: "var(--accent)",
              textDecoration: "underline",
            }}
          >
            Renew plan
          </a>
        </div>
      )}

      {error && errorCode === "subscription_required" && (
        <div className="analytics-upsell-card">
          <h2 style={{ margin: 0, fontSize: "1.25rem" }}>
            Plus or Pro subscription required
          </h2>
          <p style={{ margin: 0 }}>
            Advanced learning analytics requires an active Plus or Pro
            subscription. Upgrade your plan to unlock course, daily XP, streak,
            and completion breakdowns.
          </p>
          <div className="analytics-actions">
            <a
              href="/credits"
              style={{
                display: "inline-block",
                fontWeight: 800,
                color: "var(--accent)",
                textDecoration: "underline",
              }}
            >
              View plans
            </a>
            <button
              type="button"
              onClick={handleExportHistory}
              disabled={exportingHistory}
              style={{
                cursor: "pointer",
                fontWeight: 700,
                padding: "0.5rem 1rem",
                borderRadius: "0.75rem",
                border: "2px solid var(--line)",
                background: "var(--surface)",
              }}
            >
              {exportingHistory
                ? "Preparing export…"
                : "Export learning history"}
            </button>
          </div>
          {exportNotice && <p role="status">{exportNotice}</p>}
        </div>
      )}

      {error && !errorCode && (
        <div
          role="alert"
          style={{
            padding: "1rem",
            borderRadius: "1rem",
            border: "2px solid #b3261e",
          }}
        >
          <p style={{ margin: 0, color: "#b3261e", fontWeight: 700 }}>
            {error}
          </p>
          <button
            type="button"
            onClick={handleRetry}
            style={{
              marginTop: "0.5rem",
              cursor: "pointer",
              fontWeight: 800,
              padding: "0.5rem 1rem",
              borderRadius: "0.75rem",
              border: "2px solid var(--line)",
              background: "var(--surface)",
            }}
          >
            Retry
          </button>
        </div>
      )}

      {data && (
        <>
          {/* Summary Cards */}
          <div className="analytics-grid">
            <div className="analytics-card">
              <span>Total XP</span>
              <strong>{data.summary.total_xp}</strong>
            </div>
            <div className="analytics-card">
              <span>Completed lessons</span>
              <strong>{data.summary.total_lessons_completed}</strong>
            </div>
            <div className="analytics-card">
              <span>First-try perfect</span>
              <strong>{data.summary.perfect_completions}</strong>
            </div>
            <div className="analytics-card">
              <span>Current streak</span>
              <strong>{data.summary.current_streak}</strong>
              <span>Longest: {data.summary.longest_streak} days</span>
            </div>
            <div className="analytics-card">
              <span>Streak freezes</span>
              <strong>{data.summary.freezes_held}</strong>
              <span>Used: {data.summary.freezes_used}</span>
            </div>
          </div>

          {/* Course Breakdown */}
          <div className="analytics-card">
            <h2 style={{ margin: 0, fontSize: "1.25rem" }}>Course breakdown</h2>
            {data.course_breakdown.length === 0 ? (
              <p style={{ margin: 0 }}>No course completions recorded yet.</p>
            ) : (
              <div style={{ overflowX: "auto" }}>
                <table className="analytics-table">
                  <thead>
                    <tr>
                      <th>Course</th>
                      <th>Lessons</th>
                      <th>Total XP</th>
                      <th>Perfect</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.course_breakdown.map((c) => (
                      <tr key={c.course_id}>
                        <td style={{ fontWeight: 700 }}>{c.title}</td>
                        <td>{c.completed_lessons}</td>
                        <td>{c.total_xp} XP</td>
                        <td>{c.perfect_completions}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          {/* Daily XP Breakdown */}
          <div className="analytics-card">
            <h2 style={{ margin: 0, fontSize: "1.25rem" }}>
              Daily XP activity
            </h2>
            {data.daily_xp_breakdown.length === 0 ? (
              <p style={{ margin: 0 }}>No daily activity recorded yet.</p>
            ) : (
              <div style={{ overflowX: "auto" }}>
                <table className="analytics-table">
                  <thead>
                    <tr>
                      <th>Date</th>
                      <th>XP earned</th>
                      <th>Lessons</th>
                      <th>Goal status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.daily_xp_breakdown.map((d) => (
                      <tr key={d.local_date}>
                        <td>{d.local_date}</td>
                        <td>{d.xp} XP</td>
                        <td>{d.lessons_completed}</td>
                        <td>{d.daily_goal_met ? "Goal met" : "In progress"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          {/* Export Panel */}
          <div className="analytics-export-panel">
            <h2 style={{ margin: 0, fontSize: "1.25rem" }}>Data exports</h2>
            <p style={{ margin: 0, fontSize: "0.875rem" }}>
              Exports stream immediately without server persistence or storage
              retention.
            </p>

            <div className="analytics-actions">
              <button
                type="button"
                onClick={handleExportHistory}
                disabled={exportingHistory}
                style={{
                  cursor: "pointer",
                  fontWeight: 800,
                  padding: "0.6rem 1.25rem",
                  borderRadius: "0.875rem",
                  border: "2px solid var(--line)",
                  background: "var(--surface)",
                }}
              >
                {exportingHistory ? "Exporting…" : "Export learning history"}
              </button>
            </div>

            <div style={{ marginTop: "0.75rem" }}>
              <h3 style={{ margin: "0 0 0.5rem", fontSize: "1rem" }}>
                Custom analytics export
              </h3>
              {data.custom_export_allowed ? (
                <div>
                  <div className="analytics-dimension-list">
                    {dimensionOptions.map((dim) => (
                      <label key={dim.id} className="analytics-dimension-label">
                        <input
                          type="checkbox"
                          checked={selectedDimensions[dim.id]}
                          onChange={() => toggleDimension(dim.id)}
                        />
                        {dim.label}
                      </label>
                    ))}
                  </div>
                  <button
                    type="button"
                    onClick={handleExportCustomAnalytics}
                    disabled={exportingAnalytics}
                    style={{
                      marginTop: "0.75rem",
                      cursor: "pointer",
                      fontWeight: 800,
                      padding: "0.6rem 1.25rem",
                      borderRadius: "0.875rem",
                      border: "2px solid var(--line)",
                      background: "var(--surface)",
                    }}
                  >
                    {exportingAnalytics
                      ? "Exporting…"
                      : "Export custom analytics"}
                  </button>
                </div>
              ) : (
                <p style={{ margin: 0, fontSize: "0.875rem", color: "#555" }}>
                  Pro subscription required for custom export. Upgrade to Pro to
                  select and export custom dimensions.
                </p>
              )}
            </div>

            {exportNotice && (
              <p role="status" style={{ fontWeight: 700, margin: 0 }}>
                {exportNotice}
              </p>
            )}
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
