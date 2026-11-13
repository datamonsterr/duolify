import { session } from "../accounts/session";
import { linkFailure, type EnrollResult, type LinkFailure } from "./links";

export type Sort = "popular" | "new" | "rating";
export type Creator = {
  username: string;
  display_name: string;
  avatar_url?: string;
};
export type CatalogueItem = {
  course_id: string;
  title: string;
  description: string;
  category: string;
  language: string;
  version: number;
  published_at: string;
  creator: Creator;
  lessons_ready: number;
  lessons_total: number;
  enrollment_count: number;
  rating_average: number | null;
  rating_count: number;
};
export type CataloguePage = { items: CatalogueItem[]; next_cursor: string };
export type MarketCourse = CatalogueItem & {
  tags: string[];
  outline: {
    title: string;
    lessons: { title: string; generated: boolean }[];
  }[];
  sample_slides: { id: string; html: string }[];
  viewer: { enrolled: boolean; owner: boolean; course_id?: string };
};
export type BrowseParams = {
  q?: string;
  category?: string;
  language?: string;
  sort?: Sort;
  cursor?: string;
};

/** The fixed taxonomy (the API validates the same five ids). */
export const categories = [
  { id: "science", label: "Science" },
  { id: "tech", label: "Tech" },
  { id: "languages", label: "Languages" },
  { id: "life_skills", label: "Life skills" },
  { id: "arts", label: "Arts" },
] as const;
export const languages = [
  { id: "", label: "Any language" },
  { id: "en", label: "English" },
  { id: "es", label: "Español" },
  { id: "fr", label: "Français" },
  { id: "de", label: "Deutsch" },
  { id: "pt", label: "Português" },
  { id: "vi", label: "Tiếng Việt" },
  { id: "ja", label: "日本語" },
] as const;
export const sorts: { id: Sort; label: string }[] = [
  { id: "popular", label: "Popular" },
  { id: "new", label: "New" },
  { id: "rating", label: "Rating" },
];

/** Empty filters are omitted; the sort is always sent so a cached page is never ambiguous. */
export function catalogueUrl(p: BrowseParams): string {
  const query = new URLSearchParams();
  if (p.q?.trim()) query.set("q", p.q.trim());
  if (p.category) query.set("category", p.category);
  if (p.language) query.set("language", p.language);
  query.set("sort", p.sort ?? "popular");
  if (p.cursor) query.set("cursor", p.cursor);
  return `/v1/marketplace/courses?${query}`;
}

export const ratingLabel = (item: CatalogueItem) =>
  item.rating_count > 0 && item.rating_average !== null
    ? `★ ${item.rating_average.toFixed(1)} (${item.rating_count.toLocaleString()})`
    : "New";

export function learnersLabel(n: number): string {
  if (n <= 0) return "No learners yet";
  if (n === 1) return "1 learner";
  if (n >= 1000) return `${(Math.floor(n / 100) / 10).toString()}k learners`;
  return `${n} learners`;
}

export type MarketFailure = LinkFailure;
/** Status only, never server text; 404 is the one neutral refusal. */
export const marketFailure = linkFailure;
export const marketMessages: Record<MarketFailure, string> = {
  unavailable: "This course isn't available.",
  offline: "Couldn't reach Duolify. Check your connection and retry.",
  rate_limited: "Too many requests. Wait a minute, then retry.",
};

/** The actor always comes from the session; the server decides what this viewer may see. */
export const fetchCatalogue = (p: BrowseParams) =>
  session.request<CataloguePage>(catalogueUrl(p));
export const fetchMarketCourse = (id: string) =>
  session.request<MarketCourse>(
    `/v1/marketplace/courses/${encodeURIComponent(id)}`,
  );
export const enrollMarketplace = (id: string) =>
  session.request<EnrollResult>(
    `/v1/marketplace/courses/${encodeURIComponent(id)}/enroll`,
    "POST",
    {},
  );
