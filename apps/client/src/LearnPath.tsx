import { useUiPreferences } from "./features/design/localization/Preferences";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { api } from "./lib/api";
import { RouteUnavailable } from "./features/shell/RouteBoundary";

export function LearnPath({ courseId }: { courseId: string }) {
  const { t } = useUiPreferences();
  const {
    data: course,
    isPending,
    isError,
    error,
  } = useQuery({
    queryKey: ["course", courseId],
    queryFn: () => api.course(courseId),
    retry: false,
  });

  if (isPending) return <p>{t("path.loading")}</p>;
  if (isError) {
    if (
      (error as { status?: number })?.status === 404 ||
      (error as { message?: string })?.message?.includes("404") ||
      (error as { message?: string })?.message?.includes("not found")
    ) {
      return <RouteUnavailable />;
    }
    return <p>{t("path.error")}</p>;
  }
  if (!course || course.availability === "unavailable") {
    return <RouteUnavailable />;
  }

  return (
    <>
      <div className="eyebrow">{t("path.title")}</div>
      <h1>
        <bdi>{course.title}</bdi>
      </h1>
      <p dir="auto">{course.description}</p>
      <a className="shell-link" href={`/courses/${courseId}/cast`}>
        Course Cast
      </a>
      {course.units.map((unit) => (
        <section key={unit.title}>
          <div className="unit">
            <small>{t("path.explore")}</small>
            <h2>
              <bdi>{unit.title}</bdi>
            </h2>
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
                      aria-label={t("path.start", { name: lesson.title })}
                    >
                      ▶
                    </Link>
                    <span className="path-start" aria-hidden="true">
                      {t("path.start_short")}
                    </span>
                  </>
                ) : lesson.state === "ready_to_generate" ? (
                  <button
                    className="node current"
                    disabled
                    aria-label={`Ready to generate: ${lesson.title}`}
                  >
                    ▶
                  </button>
                ) : lesson.state === "done" ? (
                  <Link
                    className="node done"
                    to="/lesson/$lessonId"
                    params={{ lessonId: lesson.id }}
                    aria-label={t("path.review", {
                      index: index + 1,
                      name: lesson.title,
                    })}
                  >
                    ✓
                  </Link>
                ) : (
                  <span
                    className={`node ${lesson.state}`}
                    role="img"
                    aria-label={t("path.locked_help", {
                      index: index + 1,
                      name: lesson.title,
                    })}
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
                  <strong>
                    <bdi>{lesson.title}</bdi>
                  </strong>
                  {lesson.goal ? (
                    <div className="lesson-goal muted">
                      <bdi>{lesson.goal}</bdi>
                    </div>
                  ) : null}
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
