import { session } from "../accounts/session";
import {
  configureCacheBounds,
  downloadLesson,
  removeCachedLesson,
  resetCacheBounds,
} from "./cache";
import { trackControl, trackPlayerRecovery } from "../../lib/analytics";
import type { Manifest } from "./manifest";
import type { Course } from "../../lib/api";

export type DownloadLessonInfo = {
  lesson_id: string;
  unit_id: string;
  title: string;
  position: number;
  status: string;
  unlocked: boolean;
  downloadable: boolean;
  manifest_url: string;
};

export type CourseDownloadResponse = {
  course_id: string;
  title: string;
  entitled: boolean;
  plan_id?: string;
  lessons: DownloadLessonInfo[];
};

export type CourseDownloadProgress = {
  courseId: string;
  completed: number;
  total: number;
  currentLessonTitle?: string;
  status: "idle" | "downloading" | "completed" | "error";
  error?: string;
};

export type DownloadedCourse = {
  courseId: string;
  title: string;
  downloadedAt: string;
  lessonIds: string[];
  courseData?: Course;
};

const STORAGE_KEY_PREFIX = "duolify:download-index:";

function storageKey(owner: string) {
  return `${STORAGE_KEY_PREFIX}${owner}`;
}

export function getDownloadedCourses(
  owner: string,
): Record<string, DownloadedCourse> {
  try {
    if (!owner) return {};
    const raw = localStorage.getItem(storageKey(owner));
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

export function saveDownloadedCourse(owner: string, course: DownloadedCourse) {
  if (!owner) return;
  try {
    const courses = getDownloadedCourses(owner);
    courses[course.courseId] = course;
    localStorage.setItem(storageKey(owner), JSON.stringify(courses));
  } catch {
    // Device storage might be restricted
  }
}

export function clearDownloadIndex(owner?: string) {
  try {
    if (owner) {
      localStorage.removeItem(storageKey(owner));
    } else {
      for (const k of Object.keys(localStorage)) {
        if (k.startsWith(STORAGE_KEY_PREFIX)) {
          localStorage.removeItem(k);
        }
      }
    }
  } catch {
    // Ignore storage clearing issues
  }
}

export function isLessonSavedOffline(owner: string, lessonId: string): boolean {
  if (!owner || !lessonId) return false;
  const courses = getDownloadedCourses(owner);
  for (const c of Object.values(courses)) {
    if (c.lessonIds && c.lessonIds.includes(lessonId)) return true;
  }
  return false;
}

export async function removeCourseFromStorage(
  owner: string,
  courseId: string,
): Promise<void> {
  const courses = getDownloadedCourses(owner);
  const course = courses[courseId];
  if (course) {
    for (const lessonId of course.lessonIds) {
      await removeCachedLesson(owner, lessonId);
    }
    delete courses[courseId];
    try {
      localStorage.setItem(storageKey(owner), JSON.stringify(courses));
    } catch {
      /* ignore storage write failures */
    }
  }
}

export async function downloadCourseLibrary(
  courseId: string,
  onProgress?: (progress: CourseDownloadProgress) => void,
  courseData?: Course,
): Promise<{ success: boolean; downloadedCount: number; error?: string }> {
  const owner = session.current()?.id;
  if (!owner) {
    return { success: false, downloadedCount: 0, error: "sign_in_required" };
  }

  // Analytics: ui_control_activated(download_library)
  trackControl("download_library", courseId);

  // Configure bounded cache for bulk library download
  configureCacheBounds({ maxLessons: 50, maxAccount: 100 * 1024 * 1024 });
  try {
    onProgress?.({
      courseId,
      completed: 0,
      total: 0,
      status: "downloading",
    });

    let library: CourseDownloadResponse;
    try {
      library = await session.request<CourseDownloadResponse>(
        `/v1/courses/${encodeURIComponent(courseId)}/download`,
      );
    } catch (err: unknown) {
      const status = (err as { status?: number })?.status;
      const errorCode =
        status === 403 ? "subscription_required" : "download_failed";
      trackPlayerRecovery("retry", errorCode);
      onProgress?.({
        courseId,
        completed: 0,
        total: 0,
        status: "error",
        error: errorCode,
      });
      return { success: false, downloadedCount: 0, error: errorCode };
    }

    const downloadable = library.lessons.filter((l) => l.downloadable);
    const total = downloadable.length;
    if (total === 0) {
      onProgress?.({
        courseId,
        completed: 0,
        total: 0,
        status: "completed",
      });
      return { success: true, downloadedCount: 0 };
    }

    let completed = 0;
    const downloadedIds: string[] = [];

    for (const item of downloadable) {
      onProgress?.({
        courseId,
        completed,
        total,
        currentLessonTitle: item.title,
        status: "downloading",
      });

      try {
        // 1. Fetch manifest
        const manifest = await session.request<Manifest>(item.manifest_url);
        // 2. Download and verify lesson bundle into cache
        await downloadLesson(manifest, owner);
        completed++;
        downloadedIds.push(item.lesson_id);
      } catch {
        trackPlayerRecovery("retry", "download_failed");
        onProgress?.({
          courseId,
          completed,
          total,
          currentLessonTitle: item.title,
          status: "error",
          error: "download_failed",
        });
        return {
          success: false,
          downloadedCount: completed,
          error: "download_failed",
        };
      }
    }

    let courseDataToSave = courseData;
    if (!courseDataToSave) {
      try {
        courseDataToSave = await session.request<Course>(
          `/v1/courses/${encodeURIComponent(courseId)}`,
        );
      } catch {
        /* course data optional for offline store */
      }
    }

    // Record course in download index
    saveDownloadedCourse(owner, {
      courseId,
      title: library.title,
      downloadedAt: new Date().toISOString(),
      lessonIds: downloadedIds,
      courseData: courseDataToSave,
    });

    onProgress?.({
      courseId,
      completed: total,
      total,
      status: "completed",
    });

    return { success: true, downloadedCount: completed };
  } finally {
    resetCacheBounds();
  }
}
