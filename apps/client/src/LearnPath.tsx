import { SteerCourse } from "./features/steering/SteerCourse";
import { CourseUpdate } from "./features/sharing/CourseUpdate";
import { ShareCourse } from "./features/sharing/ShareCourse";
import { MissingLessons } from "./features/sharing/MissingLessons";
import { PreviewPublicationDraft } from "./features/sharing/PreviewPublicationDraft";
import { useEffect, useState } from "react";
import { useUiPreferences } from "./features/design/localization/Preferences";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useLocation } from "@tanstack/react-router";
import { api } from "./lib/api";
import { AuthError, session } from "./features/accounts/session";
import { RouteUnavailable } from "./features/shell/RouteBoundary";
import { useLessonTestTracerEnabled } from "./lib/features";
import {
  downloadCourseLibrary,
  getDownloadedCourses,
  removeCourseFromStorage,
  type CourseDownloadProgress,
} from "./features/player/library";
import { availableOffline } from "./features/player/cache";
import { TutorChat } from "./features/chat/TutorChat";
import { BlockedCourseNotice } from "./features/sharing/BlockedCopy";
import { CopyOriginNotice } from "./features/sharing/CopyOriginNotice";

export function LearnPath({ courseId }: { courseId: string }) {
  const returnTo = useLocation({ select: (location) => location.pathname });
  const { t } = useUiPreferences();
  const queryClient = useQueryClient();
  const tracerEnabled = useLessonTestTracerEnabled();
  const [preparingId, setPreparingId] = useState<string | null>(null);

  const [isOnline, setIsOnline] = useState(
    typeof navigator !== "undefined" ? navigator.onLine : true,
  );

  useEffect(() => {
    const handleOnline = () => setIsOnline(true);
    const handleOffline = () => setIsOnline(false);
    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);
    return () => {
      window.removeEventListener("online", handleOnline);
      window.removeEventListener("offline", handleOffline);
    };
  }, []);

  const [downloadProgress, setDownloadProgress] =
    useState<CourseDownloadProgress | null>(null);
  const [, setStorageVersion] = useState(0);

  const owner = session.current()?.id || "";
  // A published copy asks for a quote and consent first (MissingLessons), never a one-click prepare.
  const downloadedCourses = getDownloadedCourses(owner);
  const downloadedCourse = downloadedCourses[courseId];

  const {
    data: course,
    isPending,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ["course", courseId, owner],
    queryFn: async () => {
      try {
        const c = await api.course(courseId);
        return c;
      } catch (err) {
        if (
          err instanceof AuthError ||
          !(err instanceof TypeError || !navigator.onLine)
        )
          throw err;
        const downloaded = getDownloadedCourses(owner)[courseId];
        if (downloaded?.courseData) {
          return downloaded.courseData;
        }
        throw err;
      }
    },
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

  const handleDownload = async () => {
    setDownloadProgress({
      courseId,
      completed: 0,
      total: 0,
      status: "downloading",
    });
    const result = await downloadCourseLibrary(
      courseId,
      (prog) => setDownloadProgress(prog),
      course,
    );
    setStorageVersion((v) => v + 1);
    if (!result.success && result.error) {
      setDownloadProgress({
        courseId,
        completed: 0,
        total: 0,
        status: "error",
        error: result.error,
      });
    }
  };

  const handleRemove = async () => {
    await removeCourseFromStorage(owner, courseId);
    setStorageVersion((v) => v + 1);
    setDownloadProgress(null);
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
  if (course.moderation_blocked) return <BlockedCourseNotice />;
  const directPrepare = tracerEnabled && !course.pinned_version;

  return (
    <>
      <div className="eyebrow">{t("path.title")}</div>
      <h1>
        <bdi>{course.title}</bdi>
      </h1>
      <p dir="auto">{course.description}</p>
      <CopyOriginNotice origin={course.copy_origin} />
      {course.pinned_version ? (
        <p className="muted" data-testid="pinned-version">
          Published course · version {course.pinned_version}
        </p>
      ) : null}
      {course.pinned_version ? (
        <CourseUpdate
          courseId={courseId}
          onUpdated={() => {
            refetch();
            queryClient.invalidateQueries({ queryKey: ["course", courseId] });
          }}
        />
      ) : null}
      {course.pinned_version ? (
        <MissingLessons
          courseId={courseId}
          onChanged={() => {
            refetch();
            queryClient.invalidateQueries({ queryKey: ["course", courseId] });
          }}
        />
      ) : null}
      <a className="shell-link" href={`/courses/${courseId}/cast`}>
        Course Cast
      </a>
      {(!course.visibility || course.visibility === "private") && (
        <SteerCourse key={courseId} courseId={courseId} />
      )}
      <PreviewPublicationDraft courseId={courseId} />
      <ShareCourse courseId={courseId} />
      <div>
        <TutorChat courseId={courseId} />
      </div>

      {/* Offline banner matching S08-Path canvas */}
      {!isOnline && (
        <div
          className="ban warn"
          role="status"
          style={{ marginBlock: "var(--space-4)" }}
        >
          <svg
            className="node-icon"
            aria-hidden="true"
            viewBox="0 0 24 24"
            style={{ width: "20px", height: "20px", flexShrink: 0 }}
          >
            <path d="M1 1l22 22M16.72 11.06A10.94 10.94 0 0 1 19 12.55M5 12.55a10.94 10.94 0 0 1 5.17-2.39M10.71 5.05A16 16 0 0 1 22.58 9M1.42 9a15.91 15.91 0 0 1 4.7-2.88M8.53 16.11a6 6 0 0 1 6.95 0M12 20h.01" />
          </svg>
          <div>
            <b>Offline. Saved lessons are available.</b>
            <p>Progress syncs when you reconnect.</p>
          </div>
        </div>
      )}

      {/* Course library download and storage controls */}
      <div
        className="course-offline-controls"
        style={{
          marginBlock: "var(--space-4)",
          display: "flex",
          flexDirection: "column",
          gap: "var(--space-3)",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: "var(--space-3)",
            flexWrap: "wrap",
          }}
        >
          <button
            type="button"
            className="btn"
            onClick={handleDownload}
            disabled={downloadProgress?.status === "downloading"}
            aria-label="Download library"
          >
            {downloadProgress?.status === "downloading"
              ? "Downloading library…"
              : downloadedCourse
                ? "Update offline library"
                : "Download library"}
          </button>
          {downloadedCourse && (
            <button
              type="button"
              className="btn sec"
              onClick={handleRemove}
              aria-label="Remove downloaded course from storage"
            >
              Remove from storage
            </button>
          )}
        </div>

        {downloadProgress?.status === "downloading" && (
          <div
            role="status"
            aria-live="polite"
            style={{
              display: "flex",
              flexDirection: "column",
              gap: "var(--space-1)",
            }}
          >
            <progress
              value={downloadProgress.completed}
              max={Math.max(downloadProgress.total, 1)}
              aria-label="Download progress"
            />
            <small>
              Downloading {downloadProgress.completed} of{" "}
              {downloadProgress.total} lessons…
            </small>
          </div>
        )}

        {downloadProgress?.status === "completed" && (
          <div
            role="status"
            style={{ color: "var(--success)", fontWeight: "bold" }}
          >
            Saved offline
          </div>
        )}

        {downloadProgress?.status === "error" && (
          <div role="alert" className="ban bad">
            <div>
              <b>
                {downloadProgress.error === "subscription_required"
                  ? "Subscription required to download offline library."
                  : "Download failed. Please check connection and try again."}
              </b>
              <p>
                {downloadProgress.error === "subscription_required"
                  ? "An active Learn, Plus, or Pro subscription is required to download an offline library."
                  : "Your downloaded lessons remain available offline."}
              </p>
              {downloadProgress.error === "subscription_required" && (
                <div style={{ marginBlockStart: "var(--space-2)" }}>
                  <Link
                    to="/credits"
                    className="btn vio"
                    style={{ fontSize: "12px", minHeight: "36px" }}
                  >
                    See subscription options
                  </Link>
                </div>
              )}
            </div>
          </div>
        )}

        {downloadedCourse && (
          <div
            className="muted"
            style={{ fontSize: "var(--font-size-caption)" }}
          >
            Stored offline: {downloadedCourse.lessonIds.length} lessons
            downloaded.
          </div>
        )}
      </div>
      {course.units.map((unit) => (
        <section key={unit.title}>
          <div className="unit">
            <small>{t("path.explore")}</small>
            <h2>
              <bdi>{unit.title}</bdi>
            </h2>
          </div>
          <div className="path">
            {unit.lessons.map((lesson, index) => {
              const isOffline =
                downloadedCourse?.lessonIds?.includes(lesson.id) ||
                availableOffline(lesson.id);
              return (
                <div className="node-row" key={lesson.id}>
                  {lesson.state === "current" ? (
                    <>
                      <Link
                        className="node current"
                        to="/lesson/$lessonId"
                        params={{ lessonId: lesson.id }}
                        search={{ returnTo }}
                        aria-label={t("path.start", { name: lesson.title })}
                      >
                        ▶
                      </Link>
                      <span className="path-start" aria-hidden="true">
                        {t("path.start_short")}
                      </span>
                    </>
                  ) : lesson.state === "ready_to_generate" && directPrepare ? (
                    <button
                      className="node ready"
                      onClick={() => handlePrepareLesson(lesson.id)}
                      disabled={preparingId === lesson.id}
                      aria-label={`Lesson ${index + 1}, ${lesson.title}, ready to prepare`}
                    >
                      {preparingId === lesson.id ? "…" : "▶"}
                    </button>
                  ) : lesson.state === "generating" && tracerEnabled ? (
                    <span
                      className="node blue"
                      role="img"
                      aria-label={`Lesson ${index + 1}, preparing`}
                    >
                      ⏳
                    </span>
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
                      search={{ returnTo }}
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
                    {isOffline && (
                      <div>
                        <span className="saved-offline-tag">Saved offline</span>
                      </div>
                    )}
                    {lesson.goal ? (
                      <div className="lesson-goal muted">
                        <bdi>{lesson.goal}</bdi>
                      </div>
                    ) : null}
                    <div className="muted">
                      {lesson.state === "locked" ||
                      (!tracerEnabled &&
                        (lesson.state === "ready_to_generate" ||
                          lesson.state === "generating"))
                        ? t("path.locked")
                        : lesson.state === "done"
                          ? "Completed · Review"
                          : lesson.state === "ready_to_generate"
                            ? "Ready to prepare"
                            : lesson.state === "generating"
                              ? "Preparing…"
                              : t("path.ready")}
                    </div>
                    {lesson.reused && lesson.state !== "done" && (
                      <div className="muted" data-testid="lesson-reused">
                        Free · from the published course
                      </div>
                    )}
                    {lesson.state === "ready_to_generate" && directPrepare && (
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
              );
            })}
          </div>
        </section>
      ))}
    </>
  );
}
