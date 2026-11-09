import { useUiPreferences } from "./features/design/localization/Preferences";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { api } from "./lib/api";
export function LearnPath({ courseId }: { courseId: string }) {
  const { t } = useUiPreferences();
  const {
    data: course,
    isPending,
    isError,
  } = useQuery({
    queryKey: ["course", courseId],
    queryFn: () => api.course(courseId),
  });
  if (isPending) return <p>{t("path.loading")}</p>;
  if (isError) return <p>{t("path.error")}</p>;
  return (
    <>
      <div className="eyebrow">{t("path.title")}</div>
      <h1>
        <bdi>{course.title}</bdi>
      </h1>
      <p dir="auto">{course.description}</p>
      {course.units.map((unit) => (
        <section key={unit.title}>
          <div className="unit">
            <small>{t("path.explore")}</small>
            <h2>
              <bdi>{unit.title}</bdi>
            </h2>
          </div>
          <div className="path">
            {unit.lessons.map((lesson) => (
              <div className="node-row" key={lesson.id}>
                {lesson.state === "current" ? (
                  <Link
                    className="node current"
                    to="/lesson/$lessonId"
                    params={{ lessonId: lesson.id }}
                    aria-label={t("path.start", { name: lesson.title })}
                  >
                    ✦
                  </Link>
                ) : (
                  <span className={`node ${lesson.state}`}>
                    {lesson.state === "done" ? "✓" : "●"}
                  </span>
                )}
                <div>
                  <strong>
                    <bdi>{lesson.title}</bdi>
                  </strong>
                  <div className="muted">
                    {lesson.state === "locked"
                      ? t("path.locked")
                      : lesson.state === "done"
                        ? t("path.completed")
                        : t("path.ready")}
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
