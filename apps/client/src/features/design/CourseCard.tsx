import { ModalDialog } from "./sheet/ModalDialog";
import { ActionFooter } from "./sheet/ActionFooter";
import { useEscapeLayer } from "../hotkeys/escape";
import { useUiPreferences } from "./localization/Preferences";
import { useId, useState } from "react";
import {
  summarizeCourse,
  type Course,
  type CourseSummary,
} from "../../lib/api";
import { trackControl } from "../../lib/analytics";
import { Badge } from "./Badge";
import { AppLink } from "../../lib/navigation";

export function CourseCard({
  course,
  current = false,
  onRemove,
}: {
  course: Course | CourseSummary;
  current?: boolean;
  onRemove?: (id: string) => Promise<unknown>;
}) {
  const { t } = useUiPreferences();
  const heading = useId();
  const [showConfirm, setShowConfirm] = useState(false);
  const [removing, setRemoving] = useState(false);

  useEscapeLayer(
    "sheet",
    showConfirm ? () => setShowConfirm(false) : undefined,
    "courses",
  );

  const { progress } = summarizeCourse(course);
  const lessonCount = progress.lessons_total;
  const completed = progress.lessons_done;
  const where = progress.current;
  const currentUnitIndex = where?.unit_index ?? -1;
  const currentLessonIndex = where?.lesson_index ?? -1;
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
            <AppLink
              className="course-title"
              href={`/course/${encodeURIComponent(course.id)}`}
              onClick={() => trackControl("open_course", course.id)}
            >
              <bdi>{course.title}</bdi>
            </AppLink>
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
        lessonCount > 0 && (
          <>
            <progress
              aria-label={t("course.progress", { name: course.title })}
              value={completed}
              max={lessonCount}
            />
            <span className="muted course-progress-copy">
              {where
                ? t("course.position", {
                    unit: currentUnitIndex >= 0 ? currentUnitIndex + 1 : 1,
                    lesson: lessonIndex + 1,
                    count: where.unit_lessons,
                  })
                : t("course.completed", { completed, count: lessonCount })}
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
        <ModalDialog
          onClose={() => setShowConfirm(false)}
          aria-labelledby={`${heading}-remove-title`}
          aria-describedby={`${heading}-remove-desc`}
          className="course-remove-dialog"
        >
          <h3 id={`${heading}-remove-title`}>Remove course</h3>
          <p id={`${heading}-remove-desc`}>
            This course will be hidden immediately and permanently purged after
            30 days.
          </p>
          <ActionFooter className="course-remove-dialog-actions">
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
          </ActionFooter>
        </ModalDialog>
      )}
    </article>
  );
}
