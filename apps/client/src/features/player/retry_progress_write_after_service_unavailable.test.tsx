import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthError, authRequest, session } from "../accounts/session";
import {
  enqueueSyncEvent,
  readSyncQueue,
  synchronizeProgress,
  type PlaybackSyncEvent,
} from "./sync";

const owner = "a1111111-1111-4111-8111-111111111111";
const lessonId = "f3333333-3333-4333-8333-333333333333";
const event: PlaybackSyncEvent = {
  action_key: "act-503",
  lesson_id: lessonId,
  version: 1,
  slide_id: "sky-1",
  time_ms: 900,
  occurred_at: new Date().toISOString(),
};

beforeEach(() => {
  localStorage.clear();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.clear();
});

describe("progress writes during a read-only or switchover window", () => {
  it("surfaces Retry-After from a 503 response", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ error: "read-only" }), {
            status: 503,
            headers: { "Retry-After": "7" },
          }),
      ),
    );
    const error = await authRequest("/v1/x", {}, "token").catch((e) => e);
    expect(error).toBeInstanceOf(AuthError);
    expect(error.status).toBe(503);
    expect(error.retryAfter).toBe(7);
  });

  it("retries the same queued progress write after Retry-After and then acknowledges it", async () => {
    enqueueSyncEvent(owner, event);
    const request = vi
      .spyOn(session, "request")
      .mockRejectedValueOnce(new AuthError("read-only", 503, undefined, 3))
      .mockResolvedValueOnce({
        synced: true,
        action_keys: [event.action_key],
      } as never);
    const done = synchronizeProgress(owner, lessonId);
    await vi.advanceTimersByTimeAsync(2900);
    expect(request).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(200);
    await done;
    expect(request).toHaveBeenCalledTimes(2);
    expect(request.mock.calls[1][2]).toEqual(request.mock.calls[0][2]);
    expect(readSyncQueue(owner, lessonId)).toHaveLength(0);
  });

  it("gives up after bounded retries and keeps the event queued", async () => {
    enqueueSyncEvent(owner, event);
    const request = vi
      .spyOn(session, "request")
      .mockRejectedValue(new AuthError("read-only", 503, undefined, 1));
    const done = synchronizeProgress(owner, lessonId).catch((e) => e);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(await done).toBeInstanceOf(AuthError);
    expect(request.mock.calls.length).toBeGreaterThan(1);
    expect(request.mock.calls.length).toBeLessThanOrEqual(4);
    expect(readSyncQueue(owner, lessonId)).toHaveLength(1);
  });

  it("does not retry other failures", async () => {
    enqueueSyncEvent(owner, event);
    const request = vi
      .spyOn(session, "request")
      .mockRejectedValue(new AuthError("bad", 400));
    await expect(synchronizeProgress(owner, lessonId)).rejects.toThrow("bad");
    expect(request).toHaveBeenCalledTimes(1);
  });
});
