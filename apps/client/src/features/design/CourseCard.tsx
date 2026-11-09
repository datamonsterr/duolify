import { useUiPreferences } from "./localization/Preferences";
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
  const { t } = useUiPreferences();
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
            <bdi>{course.title}</bdi>
          ) : (
            <a
              className="course-title"
              href={`/course/${encodeURIComponent(course.id)}`}
              onClick={() => trackControl("open_course", course.id)}
            >
              <bdi>{course.title}</bdi>
            </a>
          )}
        </h2>
      </div>
      {current && !unavailable && (
        <Badge role="info">{t("course.current")}</Badge>
      )}
      <p dir="auto">{course.description}</p>
      {unavailable ? (
        <p className="unavailable-copy">{t("course.unavailable")}</p>
      ) : (
        lessons.length > 0 && (
          <>
            <progress
              aria-label={t("course.progress", { name: course.title })}
              value={completed}
              max={lessons.length}
            />
            <span className="muted course-progress-copy">
              {currentLessonIndex >= 0 && currentUnit
                ? t("course.position", {
                    unit: currentUnitIndex >= 0 ? currentUnitIndex + 1 : 1,
                    lesson: lessonIndex + 1,
                    count: currentUnit.lessons.length,
                  })
                : t("course.completed", { completed, count: lessons.length })}
            </span>
          </>
        )
      )}
    </article>
  );
}
