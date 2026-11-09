import { useId } from "react";
import type { Course } from "../../lib/api";
import { trackControl } from "../../lib/analytics";
import { Badge } from "./Badge";

export function CourseCard({
  course,
  current = false,
}: {
  course: Course;
  current?: boolean;
}) {
  const heading = useId();
  const lessons = course.units.flatMap((unit) => unit.lessons);
  const completed = lessons.filter((lesson) => lesson.state === "done").length;
  const unavailable = course.availability === "unavailable";
  return (
    <article
      className={`course-card bg-surface rounded-duo-lg ${current ? "course-current" : ""}`}
      aria-labelledby={heading}
    >
      <div className="course-card-heading">
        <span
          className={`course-icon ${current ? "course-icon-secondary" : ""}`}
          aria-hidden="true"
        >
          {current ? "✦" : "▱"}
        </span>
        <h2 id={heading}>
          {unavailable ? (
            course.title
          ) : (
            <a
              className="course-title"
              href={`/course/${encodeURIComponent(course.id)}`}
              onClick={() => trackControl("open_course", course.id)}
            >
              {course.title}
            </a>
          )}
        </h2>
      </div>
      {current && !unavailable && <Badge role="info">Current</Badge>}
      <p>{course.description}</p>
      {unavailable ? (
        <p className="unavailable-copy">Course unavailable</p>
      ) : (
        lessons.length > 0 && (
          <>
            <progress
              aria-label={`Completed lessons in ${course.title}`}
              value={completed}
              max={lessons.length}
            />
            <span className="muted">
              {completed} of {lessons.length} lessons completed
            </span>
          </>
        )
      )}
    </article>
  );
}
