import { useCallback, useEffect, useRef, useState } from "react";
import {
  enrollFailure,
  enrollInCourse,
  enrollMessages,
  existingEnrollment,
  type EnrollResult,
  type LinkFailure,
} from "./links";

type State =
  | { kind: "checking" }
  | { kind: "idle" }
  | { kind: "pending" }
  | { kind: "done"; course: string; created: boolean; result?: EnrollResult }
  | { kind: "failed"; failure: LinkFailure; retry: "check" | "enroll" };

/**
 * Enrollment on the public link page. Signed-out visitors sign in first and return here;
 * ready lessons are free and the rest are generated for credits.
 */
export function EnrollPanel({
  token,
  signedIn,
  ready,
  total,
}: {
  token: string;
  signedIn: boolean | null;
  ready: number;
  total: number;
}) {
  const [state, setState] = useState<State>({ kind: "checking" });
  const busy = useRef(false);
  const open = useRef<HTMLAnchorElement>(null);

  // The state starts as "checking", so the first read needs no state write of its own.
  const read = useCallback(() => {
    existingEnrollment(token).then(
      (r) =>
        setState(
          r.enrolled && r.course_id
            ? { kind: "done", course: r.course_id, created: false }
            : { kind: "idle" },
        ),
      (error: unknown) =>
        setState({
          kind: "failed",
          failure: enrollFailure(error),
          retry: "check",
        }),
    );
  }, [token]);
  const check = () => {
    setState({ kind: "checking" });
    read();
  };
  useEffect(() => {
    if (signedIn) read();
  }, [signedIn, read]);

  const enroll = () => {
    if (busy.current) return;
    busy.current = true;
    setState({ kind: "pending" });
    enrollInCourse(token)
      .then(
        (result) => {
          setState({
            kind: "done",
            course: result.course_id,
            created: result.created,
            result,
          });
          queueMicrotask(() => open.current?.focus());
        },
        (error: unknown) =>
          setState({
            kind: "failed",
            failure: enrollFailure(error),
            retry: "enroll",
          }),
      )
      .finally(() => (busy.current = false));
  };

  return (
    <section className="enroll-panel" aria-label="Enroll">
      <p data-testid="lessons-ready">
        {ready} of {total} lessons ready
      </p>
      <p className="sharing-note">
        Ready lessons are free. The rest are generated as you learn and cost
        credits.
      </p>
      {signedIn === false && (
        <div className="sharing-actions">
          <a
            className="duo-button"
            href={`/sign-in?returnTo=${encodeURIComponent(`/c/${token}`)}`}
          >
            Sign in to enroll
          </a>
        </div>
      )}
      {signedIn && state.kind === "checking" && (
        <p role="status">
          <span className="sharing-spinner" aria-hidden="true" />
          Checking your courses…
        </p>
      )}
      {(state.kind === "idle" || state.kind === "pending") && signedIn && (
        <div className="sharing-actions">
          <button
            type="button"
            className="duo-button"
            disabled={state.kind === "pending"}
            aria-busy={state.kind === "pending"}
            onClick={enroll}
          >
            Enroll
          </button>
        </div>
      )}
      {state.kind === "done" && (
        <div className="enroll-done" role="status">
          <p data-testid="enroll-status">
            {state.created ? "Enrolled" : "Already enrolled"}
          </p>
          {state.result && (
            <p className="sharing-meta">
              Pinned to version {state.result.version} ·{" "}
              {state.result.ready_lessons} of {state.result.total_lessons}{" "}
              lessons ready
            </p>
          )}
          <div className="sharing-actions">
            <a
              ref={open}
              className="duo-button"
              href={`/course/${encodeURIComponent(state.course)}`}
            >
              Open course
            </a>
          </div>
        </div>
      )}
      {state.kind === "failed" && (
        <div className="sharing-error" role="alert">
          <p>{enrollMessages[state.failure]}</p>
          {state.failure !== "unavailable" && (
            <button
              type="button"
              className="duo-button"
              onClick={state.retry === "check" ? check : enroll}
            >
              Retry
            </button>
          )}
        </div>
      )}
    </section>
  );
}
