import { useState, type FormEvent } from "react";
import { Button } from "../design/Button";

export interface CourseLevelOverrideProps {
  courseId: string;
  courseTitle: string;
  levelOverride: string;
  globalLevel: string;
  onSaveLevel: (
    courseId: string,
    level: string,
  ) => Promise<{ level_override: string } | void>;
}

export function CourseLevelOverride({
  courseId,
  courseTitle,
  levelOverride,
  globalLevel,
  onSaveLevel,
}: CourseLevelOverrideProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [currentOverride, setCurrentOverride] = useState(levelOverride);
  const [selectedLevel, setSelectedLevel] = useState(
    levelOverride || globalLevel || "beginner",
  );
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  const effectiveGlobal = globalLevel || "beginner";
  const displayLevel = currentOverride
    ? currentOverride.charAt(0).toUpperCase() + currentOverride.slice(1)
    : `Uses global (${effectiveGlobal})`;

  async function handleSave(e: FormEvent) {
    e.preventDefault();
    setPending(true);
    setError("");
    try {
      await onSaveLevel(courseId, selectedLevel);
      setCurrentOverride(selectedLevel);
      setIsEditing(false);
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Could not save course level.",
      );
    } finally {
      setPending(false);
    }
  }

  async function handleReset() {
    setPending(true);
    setError("");
    try {
      await onSaveLevel(courseId, "");
      setCurrentOverride("");
      setSelectedLevel(effectiveGlobal);
      setIsEditing(false);
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Could not reset course level.",
      );
    } finally {
      setPending(false);
    }
  }

  return (
    <article
      className="course-level-override card"
      aria-labelledby={`course-level-title-${courseId}`}
    >
      <h3 id={`course-level-title-${courseId}`}>{courseTitle}</h3>
      <p className="course-level-status">{displayLevel}</p>

      {error && (
        <p role="alert" className="feedback badge-danger">
          {error}
        </p>
      )}

      {isEditing ? (
        <form onSubmit={handleSave} className="course-level-form">
          <label htmlFor={`level-select-${courseId}`}>
            Level for {courseTitle}
          </label>
          <select
            id={`level-select-${courseId}`}
            value={selectedLevel}
            onChange={(e) => setSelectedLevel(e.target.value)}
            disabled={pending}
          >
            <option value="beginner">Beginner</option>
            <option value="intermediate">Intermediate</option>
            <option value="advanced">Advanced</option>
            <option value="expert">Expert</option>
          </select>
          <div className="course-level-actions">
            <Button type="submit" pending={pending} disabled={pending}>
              Save level for {courseTitle}
            </Button>
            <Button
              variant="secondary"
              type="button"
              disabled={pending}
              onClick={() => {
                setError("");
                setIsEditing(false);
              }}
            >
              Cancel
            </Button>
          </div>
        </form>
      ) : (
        <div className="course-level-actions">
          <Button
            variant="secondary"
            disabled={pending}
            aria-label={
              currentOverride
                ? `Change level for ${courseTitle}`
                : `Override level for ${courseTitle}`
            }
            onClick={() => {
              setError("");
              setSelectedLevel(currentOverride || effectiveGlobal);
              setIsEditing(true);
            }}
          >
            {currentOverride ? "Change level" : "Override level"}
          </Button>
          {currentOverride ? (
            <Button
              variant="quiet"
              pending={pending}
              disabled={pending}
              onClick={handleReset}
            >
              Reset level for {courseTitle}
            </Button>
          ) : null}
        </div>
      )}
    </article>
  );
}
