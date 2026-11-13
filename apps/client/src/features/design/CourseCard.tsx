import { useUiPreferences } from "./localization/Preferences";
import { useEffect, useId, useState } from "react";
import type { Course } from "../../lib/api";
import { trackControl } from "../../lib/analytics";
import { Badge } from "./Badge";

export function CourseCard({
  course,
  current = false,
  onRemove,
}: {
  course: Course;
  current?: boolean;
  onRemove?: (id: string) => Promise<unknown>;
}) {
  const { t } = useUiPreferences();
  const heading = useId();
  const [showConfirm, setShowConfirm] = useState(false);
  const [removing, setRemoving] = useState(false);

  useEffect(() => {
    if (!showConfirm) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setShowConfirm(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [showConfirm]);

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
      {onRemove && (
        <div className="course-card-actions">
          <button
            type="button"
            className="course-remove-btn"
            aria-label={`Remove course ${course.title}`}
            onClick={() => setShowConfirm(true)}
          >
            Remove course
          </button>
        </div>
      )}
      {showConfirm && onRemove && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby={`${heading}-remove-title`}
          aria-describedby={`${heading}-remove-desc`}
          className="course-remove-dialog card"
        >
          <h3 id={`${heading}-remove-title`}>Remove course</h3>
          <p id={`${heading}-remove-desc`}>
            This course will be hidden immediately and permanently purged after
            30 days.
          </p>
          <div className="course-remove-dialog-actions">
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => setShowConfirm(false)}
            >
              Cancel
            </button>
            <button
              type="button"
              className="btn btn-danger"
              disabled={removing}
              onClick={async () => {
                setRemoving(true);
                try {
                  await onRemove(course.id);
                  setShowConfirm(false);
                } finally {
                  setRemoving(false);
                }
              }}
            >
              {removing ? "Removing…" : "Confirm removal"}
            </button>
          </div>
        </div>
      )}
    </article>
  );
}
