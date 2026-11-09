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
  const currentUnitIndex = course.units.findIndex((unit) =>
    unit.lessons.some((lesson) => lesson.state === "current"),
  );
  const currentUnit =
    course.units[currentUnitIndex >= 0 ? currentUnitIndex : 0];
  const currentLessonIndex =
    currentUnit?.lessons.findIndex((lesson) => lesson.state === "current") ??
    -1;
  const lessonIndex = currentLessonIndex >= 0 ? currentLessonIndex : completed;
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
          <span className="course-icon-glyph">{current ? "✦" : "▱"}</span>
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
            <span className="muted course-progress-copy">
              {currentUnit &&
                `Unit ${currentUnitIndex >= 0 ? currentUnitIndex + 1 : 1} · `}
              {currentLessonIndex >= 0
                ? `Lesson ${lessonIndex + 1} of ${currentUnit?.lessons.length}`
                : `${completed} of ${lessons.length} lessons completed`}
            </span>
          </>
        )
      )}
    </article>
  );
}
