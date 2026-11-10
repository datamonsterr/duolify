import { useEffect, useId, useState } from "react";
import { authRequest } from "../accounts/session";
import "./gamification.css";

interface Progress {
  total_xp: number;
  daily_xp: number;
  daily_goal: number;
  goal_options: number[];
  goal_met: boolean;
  local_date: string;
  time_zone: string;
}

export function DailyGoal({ editable = false }: { editable?: boolean }) {
  const id = useId();
  const [progress, setProgress] = useState<Progress>();
  const [goal, setGoal] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    authRequest<Progress>("/v1/gamification", undefined, undefined, "GET")
      .then((data) => {
        if (active) {
          setProgress(data);
          setGoal(String(data.daily_goal));
          setError("");
        }
      })
      .catch((err: unknown) => {
        if (active)
          setError(
            err instanceof Error ? err.message : "Progress unavailable. Retry.",
          );
      });
    return () => {
      active = false;
    };
  }, [revision]);
  async function save() {
    setBusy(true);
    setError("");
    try {
      const data = await authRequest<Progress>(
        "/v1/gamification",
        { daily_goal: Number(goal) },
        undefined,
        "PUT",
      );
      setProgress(data);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Could not save your goal. Retry.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      className="xp-goal"
      aria-label="Daily goal progress"
      aria-busy={busy}
    >
      {error && (
        <div role="alert">
          <p>{error}</p>
          <button type="button" onClick={() => setRevision((x) => x + 1)}>
            Retry progress
          </button>
        </div>
      )}
      {!progress && !error && <p role="status">Loading daily goal…</p>}
      {progress && (
        <>
          <strong>{progress.goal_met ? "Daily goal met" : "Daily goal"}</strong>
          <div
            role="progressbar"
            aria-label="Daily XP goal"
            aria-valuemin={0}
            aria-valuemax={progress.daily_goal}
            aria-valuenow={Math.min(progress.daily_xp, progress.daily_goal)}
            className="xp-bar"
          >
            <span
              style={{
                width: `${Math.min(100, (progress.daily_xp / progress.daily_goal) * 100)}%`,
              }}
            />
          </div>
          <p aria-live="polite">
            {progress.daily_xp} / {progress.daily_goal} XP today
          </p>
          {editable && (
            <>
              <p>{progress.total_xp} XP earned from lessons</p>
              <p className="xp-local-date">
                {progress.local_date} · {progress.time_zone}
              </p>
              <label htmlFor={id}>Daily XP goal</label>
              <select
                id={id}
                value={goal}
                onChange={(event) => setGoal(event.target.value)}
                disabled={busy}
              >
                {!progress.goal_options.includes(progress.daily_goal) && (
                  <option value={progress.daily_goal}>
                    {progress.daily_goal} XP
                  </option>
                )}
                {progress.goal_options.map((option) => (
                  <option key={option} value={option}>
                    {option} XP
                  </option>
                ))}
              </select>
              <button type="button" disabled={busy} onClick={() => void save()}>
                {busy ? "Saving goal…" : "Save goal"}
              </button>
            </>
          )}
        </>
      )}
    </section>
  );
}

export function CompletionXP({
  lessonId,
  review,
}: {
  lessonId: string;
  review: boolean;
}) {
  const [award, setAward] = useState<{ xp: number; perfect: boolean }>();
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    authRequest<{ xp: number; perfect: boolean }>(
      `/v1/gamification/lessons/${lessonId}`,
      undefined,
      undefined,
      "GET",
    )
      .then((data) => {
        if (active) {
          setAward(data);
          setError("");
        }
      })
      .catch((err: unknown) => {
        if (active)
          setError(
            err instanceof Error ? err.message : "XP unavailable. Retry.",
          );
      });
    return () => {
      active = false;
    };
  }, [lessonId, revision]);
  return (
    <div className="xp-completion">
      {review ? (
        <>
          <strong>Review complete</strong>
          <p>XP was already earned for this lesson.</p>
        </>
      ) : (
        <>
          {!award && !error && <p role="status">Checking earned XP…</p>}
          {award && (
            <div className="xp-award">
              <span>Total XP</span>
              <strong>+{award.xp} XP</strong>
              {award.perfect && <span>First-try perfect bonus included</span>}
            </div>
          )}
        </>
      )}
      {error && (
        <div role="alert">
          <p>{error}</p>
          <button type="button" onClick={() => setRevision((x) => x + 1)}>
            Retry XP
          </button>
        </div>
      )}
      <DailyGoal key={award ? `ready-${revision}` : "pending"} />
    </div>
  );
}

export function ProgressPage() {
  return (
    <div className="xp-page">
      <h1>Your learning progress</h1>
      <p>
        Every completed lesson counts. Replaying lessons keeps your earned XP
        unchanged.
      </p>
      <DailyGoal editable />
    </div>
  );
}
