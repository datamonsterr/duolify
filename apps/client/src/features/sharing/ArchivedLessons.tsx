import { useState } from "react";
import { copyUpdates, type ArchivedLesson } from "./republish";
import "./sharing.css";

/** History of lessons the owner removed in a later version; progress and notes stay with them. */
export function ArchivedLessons({
  courseId,
  count,
}: {
  courseId: string;
  count: number;
}) {
  const [lessons, setLessons] = useState<ArchivedLesson[] | null>(null);
  const [failed, setFailed] = useState(false);
  if (count <= 0) return null;

  function load() {
    setFailed(false);
    copyUpdates
      .archived(courseId)
      .then(setLessons)
      .catch(() => setFailed(true));
  }
  return (
    <details
      className="sharing-archived"
      data-testid="archived-lessons"
      onToggle={(event) => {
        if (event.currentTarget.open && !lessons) load();
      }}
    >
      <summary>Archived lessons</summary>
      {failed && (
        <div className="sharing-error" role="alert">
          <p>Couldn't reach Duolify. Check your connection and retry.</p>
          <button type="button" className="duo-button" onClick={load}>
            Retry
          </button>
        </div>
      )}
      {lessons && (
        <ul>
          {lessons.map((lesson) => (
            <li key={`${lesson.version}-${lesson.title}`}>
              <bdi>{lesson.title}</bdi>
              <span className="sharing-note">
                {" "}
                · removed in version {lesson.version}
                {lesson.completed ? " · completed" : ""}
              </span>
            </li>
          ))}
        </ul>
      )}
    </details>
  );
}
