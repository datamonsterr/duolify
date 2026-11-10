import { useEffect, useState } from "react";
import { useUiPreferences } from "./features/design/localization/Preferences";
import { useQuery } from "@tanstack/react-query";
import { Link, useLocation } from "@tanstack/react-router";
import { api } from "./lib/api";
import { session } from "./features/accounts/session";
import { RouteUnavailable } from "./features/shell/RouteBoundary";
import {
  downloadCourseLibrary,
  getDownloadedCourses,
  removeCourseFromStorage,
  type CourseDownloadProgress,
} from "./features/player/library";
import { availableOffline } from "./features/player/cache";

export function LearnPath({ courseId }: { courseId: string }) {
  const returnTo = useLocation({ select: (location) => location.pathname });
  const { t } = useUiPreferences();

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
  const downloadedCourses = getDownloadedCourses(owner);
  const downloadedCourse = downloadedCourses[courseId];

  const {
    data: course,
    isPending,
    isError,
    error,
  } = useQuery({
    queryKey: ["course", courseId],
    queryFn: async () => {
      try {
        const c = await api.course(courseId);
        return c;
      } catch (err) {
        const downloaded = getDownloadedCourses(session.current()?.id)[
          courseId
        ];
        if (downloaded?.courseData) {
          return downloaded.courseData;
        }
        throw err;
      }
    },
    retry: false,
  });

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
                      {lesson.state === "locked"
                        ? t("path.locked")
                        : lesson.state === "done"
                          ? t("path.completed")
                          : t("path.ready")}
                    </div>
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
