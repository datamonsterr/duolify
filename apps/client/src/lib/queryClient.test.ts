import { describe, expect, it, vi } from "vitest";
import { AuthError } from "../features/accounts/session";
import {
  createQueryClient,
  DEFAULT_STALE_MS,
  shouldRetry,
} from "./queryClient";

const withStatus = (status: number) =>
  Object.assign(new Error(`API request failed: ${status}`), { status });

describe("query client defaults", () => {
  it("keeps data fresh for 30 s and does not refetch on window focus", () => {
    const queries = createQueryClient().getDefaultOptions().queries;
    expect(DEFAULT_STALE_MS).toBe(30_000);
    expect(queries?.staleTime).toBe(30_000);
    expect(queries?.refetchOnWindowFocus).toBe(false);
    expect(queries?.retry).toBe(shouldRetry);
  });

  it("does not retry 4xx, auth, failed fetches or an offline device", () => {
    for (const status of [400, 401, 403, 404, 409, 422, 429])
      expect(shouldRetry(0, withStatus(status))).toBe(false);
    expect(shouldRetry(0, new AuthError("Sign in again", 401))).toBe(false);
    expect(shouldRetry(0, new TypeError("Failed to fetch"))).toBe(false);
    const online = vi.spyOn(navigator, "onLine", "get").mockReturnValue(false);
    expect(shouldRetry(0, withStatus(503))).toBe(false);
    online.mockRestore();
  });

  it("retries a server error twice, then gives up", () => {
    expect(shouldRetry(0, withStatus(500))).toBe(true);
    expect(shouldRetry(1, withStatus(503))).toBe(true);
    expect(shouldRetry(2, withStatus(503))).toBe(false);
  });

  it("makes one request for a 404 query and none on focus", async () => {
    const client = createQueryClient();
    const queryFn = vi.fn().mockRejectedValue(withStatus(404));
    await client
      .fetchQuery({ queryKey: ["missing"], queryFn, retry: shouldRetry })
      .catch(() => {});
    expect(queryFn).toHaveBeenCalledTimes(1);
  });
});
