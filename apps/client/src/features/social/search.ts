export const minSearch = 3;
export interface FoundLearner {
  username: string;
  display_name: string;
  avatar: { initial: string };
}

/** Mirrors services/api/internal/social ParseQuery: NFKC, trim, one leading "@", lowercase. */
export function normalizeSearch(raw: string): string {
  return raw.trim().normalize("NFKC").replace(/^@/, "").toLowerCase();
}

export function searchPath(query: string): string {
  return `/v1/social/search?q=${encodeURIComponent(query)}`;
}

export type SearchFailure = "limited" | "unavailable";
export const searchMessages: Record<SearchFailure, string> = {
  limited: "Too many searches. Try again in a minute.",
  unavailable: "Search is unavailable. Check your connection and retry.",
};
export function searchFailure(error: unknown): SearchFailure {
  return (error as { status?: number }).status === 429
    ? "limited"
    : "unavailable";
}
