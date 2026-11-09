import { useState } from "react";
import { useUiPreferences } from "./features/design/localization/Preferences";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { api } from "./lib/api";
import { session } from "./features/accounts/session";
import { RouteUnavailable } from "./features/shell/RouteBoundary";

export function LearnPath({ courseId }: { courseId: string }) {
  const { t } = useUiPreferences();
  const queryClient = useQueryClient();
  const [preparingId, setPreparingId] = useState<string | null>(null);
  const {
    data: course,
    isPending,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ["course", courseId],
    queryFn: () => api.course(courseId),
    retry: false,
  });

  const handlePrepareLesson = async (lessonId: string) => {
    setPreparingId(lessonId);
    try {
      const quote = await session.request<{ id: string }>(
        "/v1/quotes",
        "POST",
        {
          action: "lesson_content",
        },
      );
      const prep = await session.request<{ id: string }>(
        `/v1/lessons/${encodeURIComponent(lessonId)}/prepare`,
        "POST",
        { quote_id: quote?.id },
      );
      if (prep?.id) {
        const base = import.meta.env.VITE_API_URL || "";
        const sseUrl = `${base}/v1/lesson-generations/${encodeURIComponent(prep.id)}/events`;
        try {
          const es = new EventSource(sseUrl);
          const finish = () => {
            es.close();
            setPreparingId(null);
            refetch();
            queryClient.invalidateQueries({ queryKey: ["course", courseId] });
          };
          es.onmessage = (event) => {
            try {
              const data = JSON.parse(event.data);
              if (data.state === "completed" || data.state === "failed") {
                finish();
              }
            } catch {
              finish();
            }
          };
          es.addEventListener("stage", (event: MessageEvent) => {
            try {
              const data = JSON.parse(event.data);
              if (data.state === "completed" || data.state === "failed") {
                finish();
              }
            } catch {
              finish();
            }
          });
          es.onerror = () => {
            finish();
          };
        } catch {
          setPreparingId(null);
          refetch();
          queryClient.invalidateQueries({ queryKey: ["course", courseId] });
        }
      } else {
        setPreparingId(null);
        refetch();
        queryClient.invalidateQueries({ queryKey: ["course", courseId] });
      }
    } catch {
      setPreparingId(null);
      refetch();
      queryClient.invalidateQueries({ queryKey: ["course", courseId] });
    }
  };

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
                    className="node ready"
                    onClick={() => handlePrepareLesson(lesson.id)}
                    disabled={preparingId === lesson.id}
                    aria-label={`Lesson ${index + 1}, ${lesson.title}, ready to prepare`}
                  >
                    {preparingId === lesson.id ? "…" : "▶"}
                  </button>
                ) : lesson.state === "generating" ? (
                  <span
                    className="node blue"
                    role="img"
                    aria-label={`Lesson ${index + 1}, preparing`}
                  >
                    ⏳
                  </span>
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
                        ? "Completed · Review"
                        : lesson.state === "ready_to_generate"
                          ? "Ready to prepare"
                          : lesson.state === "generating"
                            ? "Preparing…"
                            : t("path.ready")}
                  </div>
                  {lesson.state === "ready_to_generate" && (
                    <button
                      className="duo-button primary sm2"
                      style={{ marginTop: "6px" }}
                      onClick={() => handlePrepareLesson(lesson.id)}
                      disabled={preparingId === lesson.id}
                    >
                      {preparingId === lesson.id
                        ? "Preparing…"
                        : "Prepare lesson"}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </section>
      ))}
    </>
  );
}
