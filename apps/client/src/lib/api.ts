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
  id: string;
  title: string;
  description: string;
  level_override?: string;
  /** Published version a copy is pinned to. */
  pinned_version?: number | null;
  /** A severe takedown blocked this copy; the path shows a notice instead of lessons. */
  moderation_blocked?: boolean;
  /** Creator of a shared copy; null for a course the learner made. */
  copy_origin?: CopyOrigin | null;
  units: { id?: string; title: string; lessons: Lesson[] }[];
};
export type CoursesResponse = Course[] & { active_course_id?: string };
const base = import.meta.env.VITE_API_URL || "http://localhost:8080";
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
export const api = {
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
    let list: Course[] = [];
    let activeCourseId: string | undefined;

    try {
      const data = await session.request<
        | Course[]
        | {
            courses?: Course[];
            active_course_id?: string;
          }
      >("/v1/courses");
      if (Array.isArray(data)) {
        list = data;
      } else if (Array.isArray(data?.courses)) {
        list = data.courses;
        activeCourseId = data.active_course_id;
      }
    } catch (error) {
      if (options?.authenticatedOnly) throw error;
      const fallback = await get<
        Course[] | { courses?: Course[]; active_course_id?: string }
      >("/courses");
      if (Array.isArray(fallback)) {
        list = fallback;
      } else if (Array.isArray(fallback?.courses)) {
        list = fallback.courses;
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
    return session.request<{ active_course_id: string }>(
      `/v1/courses/${encodeURIComponent(id)}/select`,
      "POST",
    );
  },
  setCourseLevel: async (
    id: string,
    level: string,
  ): Promise<{ level_override: string }> => {
    return session.request<{ level_override: string }>(
      `/v1/courses/${encodeURIComponent(id)}/level`,
      "POST",
      { level },
    );
  },
  deleteCourse: async (id: string): Promise<{ deleted: boolean }> => {
    return session.request<{ deleted: boolean }>(
      `/v1/courses/${encodeURIComponent(id)}`,
      "DELETE",
    );
  },
  course: (id: string) =>
    /^[0-9a-f-]{36}$/.test(id)
      ? session.request<Course>(`/v1/courses/${encodeURIComponent(id)}`)
      : get<Course>(`/courses/${encodeURIComponent(id)}`),
};
