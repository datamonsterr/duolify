import { useEffect, useState } from "react";
import { session } from "../accounts/session";
import "./sharing.css";
import "./moderation.css";

/** Shown instead of the lessons of a copy that a severe takedown blocked. */
export function BlockedCourseNotice() {
  return (
    <section className="mod mod-blocked">
      <h1>This course was removed</h1>
      <p>
        A moderator removed the course this copy came from. Its lessons can't be
        opened any more. Your progress and your other courses are not affected.
      </p>
      <a className="duo-button secondary" href="/courses">
        Back to Courses
      </a>
    </section>
  );
}

/** Route target for a blocked copy. Anything that is not actually blocked stays neutral. */
export function BlockedCopy({ courseId }: { courseId: string }) {
  const [state, setState] = useState<"loading" | "blocked" | "other">(
    "loading",
  );
  useEffect(() => {
    let active = true;
    session
      .request<{ moderation_blocked?: boolean }>(
        `/v1/courses/${encodeURIComponent(courseId)}`,
      )
      .then(
        (c) => active && setState(c.moderation_blocked ? "blocked" : "other"),
        () => active && setState("other"),
      );
    return () => {
      active = false;
    };
  }, [courseId]);
  if (state === "loading")
    return (
      <section className="mod" aria-busy="true">
        <p role="status">Loading…</p>
      </section>
    );
  if (state === "blocked") return <BlockedCourseNotice />;
  return (
    <section className="mod">
      <h1>This page isn't available</h1>
      <a className="duo-button secondary" href="/courses">
        Back to Courses
      </a>
    </section>
  );
}
