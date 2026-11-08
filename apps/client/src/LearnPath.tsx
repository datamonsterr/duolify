import { useQuery } from "@tanstack/react-query";
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
            {unit.lessons.map((lesson) => (
              <div className="node-row" key={lesson.id}>
                {lesson.state === "current" ? (
                  <a
                    className="node current"
                    href={`/lesson/${lesson.id}`}
                    aria-label={`Start lesson: ${lesson.title}`}
                  >
                    ✦
                  </a>
                ) : (
                  <span className={`node ${lesson.state}`}>
                    {lesson.state === "done" ? "✓" : "●"}
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
