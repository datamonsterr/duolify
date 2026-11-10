import { useEffect, useState, useMemo } from "react";
import { session } from "../accounts/session";
import { bookmarkQueue } from "./bookmark-queue";
import { trackBookmarkOpened, trackBookmarkRemoved } from "../../lib/analytics";
import { availableOffline } from "../player/cache";

export type BookmarkItem = {
  id: string;
  learner_id?: string;
  action_key: string;
  course_id?: string | null;
  course_title?: string;
  lesson_id?: string | null;
  lesson_title?: string;
  slide_id?: number | null;
  slide_title?: string;
  target_type: "slide" | "lesson";
  available: boolean;
  status?: "device_saved" | "synced" | "removal_pending";
  created_at: string;
  updated_at?: string;
};

export function isLessonCached(lessonId?: string | null): boolean {
  if (!lessonId) return false;
  try {
    if (availableOffline(lessonId)) return true;
    return localStorage.getItem(`duolify:lesson-cached:${lessonId}`) === "true";
  } catch {
    return false;
  }
}

export function BookmarksPage() {
  const [bookmarks, setBookmarks] = useState<BookmarkItem[]>(() => {
    return bookmarkQueue
      .getQueue()
      .filter((q) => q.status !== "removal_pending")
      .map((q) => ({
        id: q.id ?? q.actionKey,
        action_key: q.actionKey,
        course_id: q.courseId,
        course_title: q.courseTitle,
        lesson_id: q.lessonId,
        lesson_title: q.lessonTitle,
        slide_id: q.slideId,
        slide_title: q.slideTitle,
        target_type: q.targetType,
        available: q.available ?? true,
        status: q.status,
        created_at: q.createdAt,
      }));
  });
  const [loading, setLoading] = useState(false);
  const [selectedCourse, setSelectedCourse] = useState<string>("all");
  const [offlineNotice, setOfflineNotice] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    const syncAndLoad = async () => {
      // 1. Process local queue actions against server if online
      const queue = bookmarkQueue.getQueue();
      for (const item of queue) {
        if (cancelled) return;
        if (item.status === "removal_pending") {
          try {
            if (item.id) {
              await session.request(`/v1/bookmarks/${item.id}`, "DELETE");
            } else {
              await session.request("/v1/bookmarks/remove", "POST", {
                lesson_id: item.lessonId,
                slide_id: item.slideId,
              });
            }
            bookmarkQueue.remove(item.actionKey);
          } catch {
            // Offline, keep in queue
          }
        } else if (item.status === "device_saved") {
          try {
            const res = await session.request<{ id: string }>(
              "/v1/bookmarks",
              "POST",
              {
                action_key: item.actionKey,
                lesson_id: item.lessonId,
                slide_id: item.slideId,
                target_type: item.targetType,
              },
            );
            if (res?.id) {
              bookmarkQueue.markSynced(item.actionKey, res.id);
            }
          } catch {
            // Offline, keep in queue
          }
        }
      }

      if (cancelled) return;

      // 2. Fetch server bookmarks
      try {
        const serverList =
          await session.request<BookmarkItem[]>("/v1/bookmarks");
        if (cancelled) return;
        if (Array.isArray(serverList)) {
          // Merge with unsynced local queued items
          const currentQueue = bookmarkQueue.getQueue();
          const serverKeys = new Set(serverList.map((b) => b.action_key));
          const unsyncedLocals: BookmarkItem[] = currentQueue
            .filter(
              (q) =>
                !serverKeys.has(q.actionKey) && q.status !== "removal_pending",
            )
            .map((q) => ({
              id: q.id ?? q.actionKey,
              action_key: q.actionKey,
              course_id: q.courseId,
              course_title: q.courseTitle,
              lesson_id: q.lessonId,
              lesson_title: q.lessonTitle,
              slide_id: q.slideId,
              slide_title: q.slideTitle,
              target_type: q.targetType,
              available: q.available ?? true,
              status: q.status,
              created_at: q.createdAt,
            }));

          setBookmarks([...unsyncedLocals, ...serverList]);
          setLoading(false);
          return;
        }
      } catch {
        // Offline fallback: load from queue
      }

      if (cancelled) return;

      const currentQueue = bookmarkQueue.getQueue();
      const localItems: BookmarkItem[] = currentQueue
        .filter((q) => q.status !== "removal_pending")
        .map((q) => ({
          id: q.id ?? q.actionKey,
          action_key: q.actionKey,
          course_id: q.courseId,
          course_title: q.courseTitle,
          lesson_id: q.lessonId,
          lesson_title: q.lessonTitle,
          slide_id: q.slideId,
          slide_title: q.slideTitle,
          target_type: q.targetType,
          available: q.available ?? true,
          status: q.status,
          created_at: q.createdAt,
        }));

      setBookmarks(localItems);
      setLoading(false);
    };

    void syncAndLoad();

    const handleOnline = () => {
      void syncAndLoad();
    };
    window.addEventListener("online", handleOnline);
    (
      window as unknown as { syncBookmarks?: () => Promise<void> }
    ).syncBookmarks = syncAndLoad;

    return () => {
      cancelled = true;
      window.removeEventListener("online", handleOnline);
      delete (window as unknown as { syncBookmarks?: () => Promise<void> })
        .syncBookmarks;
    };
  }, []);

  const courses = useMemo(() => {
    const set = new Set<string>();
    for (const b of bookmarks) {
      if (b.course_title) {
        set.add(b.course_title);
      }
    }
    return Array.from(set);
  }, [bookmarks]);

  const filteredBookmarks = useMemo(() => {
    if (selectedCourse === "all") return bookmarks;
    return bookmarks.filter((b) => b.course_title === selectedCourse);
  }, [bookmarks, selectedCourse]);

  const handleRemove = async (bm: BookmarkItem) => {
    bookmarkQueue.enqueueRemoval({
      id: bm.id,
      lessonId: bm.lesson_id ?? undefined,
      slideId: bm.slide_id ?? undefined,
    });

    setBookmarks((prev) => prev.filter((b) => b.action_key !== bm.action_key));

    trackBookmarkRemoved({
      targetType: bm.target_type,
      courseId: bm.course_id ?? undefined,
      lessonId: bm.lesson_id ?? undefined,
      slideId: bm.slide_id ?? undefined,
    });

    try {
      if (bm.id && !bm.id.startsWith("act-")) {
        await session.request(`/v1/bookmarks/${bm.id}`, "DELETE");
      }
    } catch {
      // Offline removal queued
    }
  };

  const handleOpen = (e: React.MouseEvent, bm: BookmarkItem) => {
    const cached = isLessonCached(bm.lesson_id);
    const isOnline = typeof navigator !== "undefined" ? navigator.onLine : true;

    if (!cached && !isOnline) {
      e.preventDefault();
      setOfflineNotice(
        `“${bm.lesson_title || "Lesson"}” isn't downloaded. Open it when online.`,
      );
      return;
    }

    trackBookmarkOpened({
      targetType: bm.target_type,
      courseId: bm.course_id ?? undefined,
      lessonId: bm.lesson_id ?? undefined,
      slideId: bm.slide_id ?? undefined,
    });

    if (bm.lesson_id && bm.slide_id !== null && bm.slide_id !== undefined) {
      try {
        localStorage.setItem(
          `duolify:slide:${bm.lesson_id}`,
          String(bm.slide_id),
        );
      } catch {
        // ignore
      }
    }
  };

  const pendingRemovalsCount = bookmarkQueue
    .getQueue()
    .filter((q) => q.status === "removal_pending").length;

  return (
    <div
      className="bookmarks-page"
      style={{ padding: "24px 16px", maxWidth: 800, margin: "0 auto" }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 12,
          marginBottom: 16,
        }}
      >
        <h1
          style={{ margin: 0, fontSize: 28, fontWeight: 900, color: "#2B2B2B" }}
        >
          Bookmarks
        </h1>
      </div>

      {/* Filter chips per canvas S21/S22 */}
      <div
        className="filter-chips"
        style={{
          display: "flex",
          gap: 8,
          overflowX: "auto",
          paddingBottom: 12,
          marginBottom: 16,
        }}
        role="group"
        aria-label="Course filter"
      >
        <button
          type="button"
          className={`duo-button chip ${selectedCourse === "all" ? "primary on" : "quiet"}`}
          onClick={() => setSelectedCourse("all")}
          aria-pressed={selectedCourse === "all"}
        >
          All courses
        </button>
        {courses.map((c) => (
          <button
            key={c}
            type="button"
            className={`duo-button chip ${selectedCourse === c ? "primary on" : "quiet"}`}
            onClick={() => setSelectedCourse(c)}
            aria-pressed={selectedCourse === c}
          >
            {c}
          </button>
        ))}
      </div>

      {/* Offline banner per canvas */}
      {(offlineNotice || pendingRemovalsCount > 0) && (
        <div
          className="banner badge-warning"
          role="status"
          style={{
            display: "flex",
            alignItems: "flex-start",
            gap: 12,
            padding: 12,
            borderRadius: 14,
            marginBottom: 16,
            background: "#FFF4E5",
            border: "2px solid #FFD8A8",
          }}
        >
          <div>
            <b>Offline</b>
            <p style={{ margin: "4px 0 0" }}>
              {offlineNotice ||
                `${pendingRemovalsCount} removal${pendingRemovalsCount > 1 ? "s" : ""} will sync later.`}
            </p>
          </div>
        </div>
      )}

      {/* Empty states */}
      {!loading && bookmarks.length === 0 && (
        <div
          className="empty-state"
          style={{
            textAlign: "center",
            padding: "48px 16px",
            color: "#666",
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: 8,
          }}
        >
          <span style={{ fontSize: 36 }}>🔖</span>
          <b style={{ color: "#2B2B2B", fontSize: 18 }}>Empty variant</b>
          <p style={{ margin: 0 }}>Save a slide to return to an idea.</p>
        </div>
      )}

      {!loading && bookmarks.length > 0 && filteredBookmarks.length === 0 && (
        <div
          style={{ textAlign: "center", padding: "32px 16px", color: "#666" }}
        >
          <p>
            0 bookmarks match “{selectedCourse}” ·{" "}
            <button
              type="button"
              className="duo-button quiet"
              onClick={() => setSelectedCourse("all")}
              style={{
                textDecoration: "underline",
                cursor: "pointer",
                display: "inline",
              }}
            >
              Clear filters
            </button>
          </p>
        </div>
      )}

      {/* Bookmarks list */}
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        {filteredBookmarks.map((bm) => {
          const cached = isLessonCached(bm.lesson_id);
          const lessonKey = bm.lesson_id || "";
          const slideParam =
            bm.slide_id !== null && bm.slide_id !== undefined
              ? `?slide=${bm.slide_id}`
              : "";
          const openUrl = lessonKey ? `/lesson/${lessonKey}${slideParam}` : "#";
          const isUnavailable = !bm.available;

          return (
            <div
              key={bm.action_key || bm.id}
              className="bookmark-card"
              style={{
                display: "flex",
                alignItems: "center",
                gap: 16,
                padding: 16,
                borderRadius: 16,
                border: "2px solid #EFEFEF",
                background: isUnavailable ? "#F7F7F7" : "#FFFFFF",
              }}
            >
              <div
                aria-hidden="true"
                style={{
                  width: 48,
                  height: 48,
                  borderRadius: 12,
                  background: isUnavailable
                    ? "#CFCFCF"
                    : cached
                      ? "#1677C9"
                      : "#1F7A0D",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  color: "#fff",
                  fontWeight: 900,
                  flexShrink: 0,
                }}
              >
                🔖
              </div>

              <div style={{ flex: 1, minWidth: 0 }}>
                <b
                  style={{
                    display: "block",
                    fontSize: 16,
                    color: isUnavailable ? "#555555" : "#2B2B2B",
                    marginBottom: 4,
                  }}
                >
                  {bm.lesson_title || "Lesson"}
                </b>

                {isUnavailable ? (
                  <span style={{ fontSize: 13, color: "#777777" }}>
                    This saved{" "}
                    {bm.target_type === "lesson" ? "lesson" : "slide"} is
                    unavailable
                  </span>
                ) : (
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 8,
                      flexWrap: "wrap",
                      fontSize: 13,
                      color: "#666",
                    }}
                  >
                    {bm.slide_id !== null && bm.slide_id !== undefined && (
                      <span>Slide {bm.slide_id + 1}</span>
                    )}
                    {cached ? (
                      <span
                        className="chip"
                        style={{
                          fontSize: 11,
                          fontWeight: 800,
                          padding: "2px 8px",
                          borderRadius: 12,
                          background: "#FEF3C7",
                          color: "#92400E",
                        }}
                      >
                        Saved offline
                      </span>
                    ) : (
                      <span>
                        {bm.course_title || "Course"} · not downloaded
                      </span>
                    )}
                  </div>
                )}
              </div>

              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                {!isUnavailable && (
                  <a
                    href={openUrl}
                    className="duo-button primary sm"
                    onClick={(e) => handleOpen(e, bm)}
                    aria-label={`Open ${bm.lesson_title || "lesson"}${
                      bm.slide_id !== null && bm.slide_id !== undefined
                        ? `, slide ${bm.slide_id + 1}`
                        : ""
                    }`}
                    style={{
                      padding: "8px 16px",
                      borderRadius: 12,
                      fontWeight: 800,
                      textDecoration: "none",
                    }}
                  >
                    Open
                  </a>
                )}
                <button
                  type="button"
                  className="duo-button quiet sm"
                  onClick={() => handleRemove(bm)}
                  aria-label={`Remove bookmark ${bm.lesson_title || ""}`}
                  style={{ padding: "8px 12px", borderRadius: 12 }}
                >
                  Remove
                </button>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
