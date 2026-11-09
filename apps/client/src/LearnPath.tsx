import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { api } from "./lib/api";
export function LearnPath({ courseId }: { courseId: string }) {
  const {
    data: course,
    isPending,
    isError,
  } = useQuery({
    queryKey: ["course", courseId],
    queryFn: () => api.course(courseId),
  });
  if (isPending) return <p>Loading your path…</p>;
  if (isError) return <p>Could not load this course.</p>;
  return (
    <>
      <div className="eyebrow">YOUR LEARNING PATH</div>
      <h1>{course.title}</h1>
      <p>{course.description}</p>
      {course.units.map((unit) => (
        <section key={unit.title}>
          <div className="unit">
            <small>KEEP EXPLORING</small>
            <h2>{unit.title}</h2>
          </div>
          <div className="path">
            {unit.lessons.map((lesson, index) => (
              <div className="node-row" key={lesson.id}>
                {lesson.state === "current" ? (
                  <>
                    <Link
                      className="node current"
                      to="/lesson/$lessonId"
                      params={{ lessonId: lesson.id }}
                      aria-label={`Start lesson: ${lesson.title}`}
                    >
                      ▶
                    </Link>
                    <span className="path-start" aria-hidden="true">
                      Start
                    </span>
                  </>
                ) : lesson.state === "done" ? (
                  <Link
                    className="node done"
                    to="/lesson/$lessonId"
                    params={{ lessonId: lesson.id }}
                    aria-label={`Lesson ${index + 1}, ${lesson.title}, completed. Review`}
                  >
                    ✓
                  </Link>
                ) : (
                  <span
                    className={`node ${lesson.state}`}
                    role="img"
                    aria-label={`Lesson ${index + 1}, ${lesson.title}, locked. Pass the previous lesson test first`}
                  >
                    <svg
                      className="node-icon"
                      viewBox="0 0 24 24"
                      aria-hidden="true"
                    >
                      <rect x="4" y="10" width="16" height="11" rx="2" />
                      <path d="M8 10V7a4 4 0 0 1 8 0v3M12 14v3" />
                    </svg>
                  </span>
                )}
                <div>
                  <strong>{lesson.title}</strong>
                  <div className="muted">
                    {lesson.state === "locked"
                      ? "Locked"
                      : lesson.state === "done"
                        ? "Completed"
                        : "Ready to start"}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </section>
      ))}
    </>
  );
}
