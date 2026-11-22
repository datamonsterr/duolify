import {
  invalidateCourseList,
  invalidateCourseProgress,
} from "./courseQueries";
import { session } from "../features/accounts/session";
import type { CopyOrigin } from "../features/sharing/retention";
import { isValidPricing, type Pricing } from "../features/pricing/types";
export type Lesson = {
  id: string;
  title: string;
  state: "done" | "current" | "locked" | "ready_to_generate" | "generating";
  goal?: string;
  /** "ready" lessons are playable as is; "needs_generation" ones are generated for credits. */
  content?: "ready" | "needs_generation";
  /** Arrived free with a published copy. */
  reused?: boolean;
};
export type Course = {
  availability?: "available" | "unavailable";
  visibility?: "private" | "link" | "public";
  id: string;
  title: string;
  description: string;
  level_override?: string;
  /** Published version a copy is pinned to. */
  pinned_version?: number | null;
  /** A severe takedown blocked this copy; the path shows a notice instead of lessons. */
  moderation_blocked?: boolean;
  /** Set on a copy of an authored example course (free, labelled "Example"). */
  example_slug?: string | null;
  /** Creator of a shared copy; null for a course the learner made. */
  copy_origin?: CopyOrigin | null;
  units: { id?: string; title: string; lessons: Lesson[] }[];
};
/** Where the learner is in a course; computed by the API so the list needs no lesson tree. */
export type CourseProgress = {
  lessons_total: number;
  lessons_done: number;
  /** Position of the current lesson among the non-archived units; null when none is current. */
  current: {
    unit_index: number;
    lesson_index: number;
    unit_lessons: number;
  } | null;
};
/** What GET /v1/courses returns per course: the card fields, no units or lessons. */
export type CourseSummary = Omit<Course, "units"> & {
  progress: CourseProgress;
};
export type CoursesResponse = CourseSummary[] & { active_course_id?: string };

/** Reduces a full course (offline copy, older mock) to the summary the list works with. */
export function summarizeCourse(course: Course | CourseSummary): CourseSummary {
  if ("progress" in course && course.progress) {
    const { units: _tree, ...summary } = course as CourseSummary & {
      units?: unknown;
    };
    void _tree;
    return summary;
  }
  const { units, ...rest } = course as Course;
  const lessons = units.flatMap((unit) => unit.lessons);
  const unitIndex = units.findIndex((unit) =>
    unit.lessons.some((lesson) => lesson.state === "current"),
  );
  const lessonIndex =
    unitIndex >= 0
      ? units[unitIndex].lessons.findIndex((l) => l.state === "current")
      : -1;
  return {
    ...rest,
    progress: {
      lessons_total: lessons.length,
      lessons_done: lessons.filter((lesson) => lesson.state === "done").length,
      current:
        unitIndex >= 0
          ? {
              unit_index: unitIndex,
              lesson_index: lessonIndex,
              unit_lessons: units[unitIndex].lessons.length,
            }
          : null,
    },
  };
}
const base = import.meta.env.VITE_API_URL ?? "http://localhost:8080";
const pricingCacheKey = "pricing:last-known";
type PricingCache = { pricing: Pricing; etag: string | null };

export function readPricingCache(): PricingCache | undefined {
  try {
    const cached = JSON.parse(
      sessionStorage.getItem(pricingCacheKey) || "null",
    ) as PricingCache | Pricing | null;
    if (!cached) return undefined;
    // Accept the previous DTO-only cache until the session expires.
    if ("pricing" in cached && isValidPricing(cached.pricing))
      return { pricing: cached.pricing, etag: cached.etag ?? null };
    if ("version" in cached && isValidPricing(cached))
      return { pricing: cached, etag: null };
  } catch {
    /* A cache failure must not prevent a fresh server read. */
  }
  return undefined;
}

function writePricingCache(pricing: Pricing, etag: string | null) {
  try {
    sessionStorage.setItem(pricingCacheKey, JSON.stringify({ pricing, etag }));
  } catch {
    /* The catalogue remains usable when browser storage is unavailable. */
  }
}

async function get<T>(path: string): Promise<T> {
  const response = await fetch(`${base}${path}`);
  if (!response.ok) {
    const error = new Error(`API request failed: ${response.status}`);
    (error as { status?: number }).status = response.status;
    throw error;
  }
  return response.json() as Promise<T>;
}
/** One authored example course offered to new learners (GET /v1/courses/examples). */
export type ExampleCourse = {
  slug: string;
  title: string;
  description: string;
  level: string;
  units: number;
  lessons: number;
  estimated_minutes: number;
};
export type ExampleSurface = "courses" | "learn_empty" | "onboarding";
export const api = {
  exampleCourses: async (): Promise<ExampleCourse[]> =>
    (
      await session.request<{ examples: ExampleCourse[] }>(
        "/v1/courses/examples",
      )
    ).examples ?? [],
  enrollExample: async (
    slug: string,
    surface: ExampleSurface,
  ): Promise<{ course_id: string }> => {
    const result = await session.request<{ course_id: string }>(
      `/v1/courses/examples/${encodeURIComponent(slug)}/enroll`,
      "POST",
      { surface },
    );
    invalidateCourseList();
    return result;
  },
  pricing: async () => {
    const cached = readPricingCache();
    const headers = new Headers();
    if (cached?.etag) headers.set("If-None-Match", cached.etag);
    const response = await fetch(`${base}/v1/pricing`, { headers });
    if (response.status === 304) {
      if (!cached) throw new Error("Pricing cache is unavailable");
      return cached.pricing;
    }
    if (!response.ok) throw new Error(`API request failed: ${response.status}`);
    const pricing = (await response.json()) as Pricing;
    if (!isValidPricing(pricing)) throw new Error("Invalid pricing response");
    writePricingCache(pricing, response.headers.get("ETag"));
    return pricing;
  },
  courses: async (options?: {
    authenticatedOnly?: boolean;
  }): Promise<CoursesResponse> => {
    let list: CourseSummary[] = [];
    let activeCourseId: string | undefined;

    try {
      const data = await session.request<
        | (Course | CourseSummary)[]
        | {
            courses?: (Course | CourseSummary)[];
            active_course_id?: string;
          }
      >("/v1/courses");
      if (Array.isArray(data)) {
        list = data.map(summarizeCourse);
      } else if (Array.isArray(data?.courses)) {
        list = data.courses.map(summarizeCourse);
        activeCourseId = data.active_course_id;
      }
    } catch (error) {
      if (options?.authenticatedOnly) throw error;
      const fallback = await get<
        | (Course | CourseSummary)[]
        | { courses?: (Course | CourseSummary)[]; active_course_id?: string }
      >("/courses");
      if (Array.isArray(fallback)) {
        list = fallback.map(summarizeCourse);
      } else if (Array.isArray(fallback?.courses)) {
        list = fallback.courses.map(summarizeCourse);
        activeCourseId = fallback.active_course_id;
      }
    }

    if (activeCourseId) {
      try {
        if (!localStorage.getItem("duolify:active-course")) {
          localStorage.setItem("duolify:active-course", activeCourseId);
        }
      } catch {
        /* LocalStorage may be unavailable. */
      }
    }

    return Object.assign(list, { active_course_id: activeCourseId });
  },
  selectCourse: async (id: string): Promise<{ active_course_id: string }> => {
    const result = await session.request<{ active_course_id: string }>(
      `/v1/courses/${encodeURIComponent(id)}/select`,
      "POST",
    );
    invalidateCourseList();
    return result;
  },
  setCourseLevel: async (
    id: string,
    level: string,
  ): Promise<{ level_override: string }> => {
    const result = await session.request<{ level_override: string }>(
      `/v1/courses/${encodeURIComponent(id)}/level`,
      "POST",
      { level },
    );
    invalidateCourseProgress();
    return result;
  },
  deleteCourse: async (id: string): Promise<{ deleted: boolean }> => {
    const result = await session.request<{ deleted: boolean }>(
      `/v1/courses/${encodeURIComponent(id)}`,
      "DELETE",
    );
    invalidateCourseProgress();
    return result;
  },
  course: (id: string) =>
    /^[0-9a-f-]{36}$/.test(id)
      ? session.request<Course>(`/v1/courses/${encodeURIComponent(id)}`)
      : get<Course>(`/courses/${encodeURIComponent(id)}`),
};
