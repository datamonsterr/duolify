import { isValidPricing, type Pricing } from "../features/pricing/types";
export type Lesson = {
  id: string;
  title: string;
  state: "done" | "current" | "locked";
};
export type Course = {
  availability?: "available" | "unavailable";
  id: string;
  title: string;
  description: string;
  units: { title: string; lessons: Lesson[] }[];
};
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
  if (!response.ok) throw new Error(`API request failed: ${response.status}`);
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
  courses: () => get<Course[]>("/courses"),
  course: (id: string) => get<Course>(`/courses/${encodeURIComponent(id)}`),
};
