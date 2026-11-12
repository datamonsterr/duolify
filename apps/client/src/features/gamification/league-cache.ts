const key = "duolify.league.cache";
export function readLeagueCache<T>(): T | undefined {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : undefined;
  } catch {
    return undefined;
  }
}
export function writeLeagueCache(value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* The cache is a convenience; the API stays the source. */
  }
}
// Rankings include other Learners' display names: drop them on sign-out.
export function clearLeagueCache() {
  try {
    localStorage.removeItem(key);
  } catch {
    /* Storage can be unavailable. */
  }
}
