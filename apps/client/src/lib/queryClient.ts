import { QueryClient } from "@tanstack/react-query";
import { AuthError } from "../features/accounts/session";

/** Lists and lookups stay fresh this long; a query that must be live says so with its own staleTime. */
export const DEFAULT_STALE_MS = 30_000;
const MAX_RETRIES = 2;

/**
 * Retry only what a retry can fix: a 5xx or a dropped connection on a good network. A 4xx, a
 * sign-in failure, an offline device and a failed fetch (a TypeError, how fetch reports no network)
 * would fail the same way again, and the offline paths already fall back to downloaded data.
 */
export function shouldRetry(failureCount: number, error: unknown): boolean {
  if (failureCount >= MAX_RETRIES) return false;
  if (error instanceof AuthError) return false;
  const status = (error as { status?: unknown } | null)?.status;
  if (typeof status === "number" && status >= 400 && status < 500) return false;
  if (error instanceof TypeError) return false;
  if (typeof navigator !== "undefined" && navigator.onLine === false)
    return false;
  return true;
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: DEFAULT_STALE_MS,
        // Every focus used to refetch each mounted query; pages refetch on mount instead.
        refetchOnWindowFocus: false,
        retry: shouldRetry,
      },
    },
  });
}
