import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import { session } from "../accounts/session";
import { configureAnalytics, trackPlayer } from "../../lib/analytics";
import {
  enqueueSyncEvent,
  readSyncQueue,
  clearSyncQueue,
  synchronizeProgress,
  type PlaybackSyncEvent,
} from "./sync";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));

const owner = "a1111111-1111-4111-8111-111111111111";
const lessonId = "f3333333-3333-4333-8333-333333333333";

beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  vi.spyOn(session, "current").mockReturnValue({
    id: owner,
    display_name: "Test Learner",
    locale: "en",
    time_zone: "UTC",
    background: "",
    analytics_opt_out: false,
  });
  configureAnalytics({
    learnerId: owner,
    optedOut: false,
    platform: "web",
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  configureAnalytics(null);
  localStorage.clear();
  sessionStorage.clear();
});

describe("offline playback progress synchronization", () => {
  it("enqueues progress with a stable action key and retains it until server ack", async () => {
    const event: PlaybackSyncEvent = {
      action_key: "act-test-001",
      lesson_id: lessonId,
      version: 1,
      slide_id: "sky-1",
      time_ms: 1200,
      occurred_at: new Date().toISOString(),
    };

    enqueueSyncEvent(owner, event);
    const queue = readSyncQueue(owner, lessonId);
    expect(queue).toHaveLength(1);
    expect(queue[0].action_key).toBe("act-test-001");
    expect(queue[0].slide_id).toBe("sky-1");

    // Simulating server sync failure keeps the event in the queue
    vi.spyOn(session, "request").mockRejectedValueOnce(new Error("offline"));
    await expect(synchronizeProgress(owner, lessonId)).rejects.toThrow(
      "offline",
    );
    expect(readSyncQueue(owner, lessonId)).toHaveLength(1);

    // Simulating server ack removes the event
    vi.spyOn(session, "request").mockResolvedValueOnce({
      synced: true,
      action_keys: ["act-test-001"],
    });
    await synchronizeProgress(owner, lessonId);
    expect(readSyncQueue(owner, lessonId)).toHaveLength(0);
  });

  it("reconnects twice and observes only one progress action", async () => {
    const event: PlaybackSyncEvent = {
      action_key: "act-test-once",
      lesson_id: lessonId,
      version: 1,
      slide_id: "sky-2",
      time_ms: 2000,
      occurred_at: new Date().toISOString(),
    };
    enqueueSyncEvent(owner, event);

    let serverCalls = 0;
    vi.spyOn(session, "request").mockImplementation(
      async (_path, _method, body) => {
        serverCalls++;
        const data = body as {
          events?: PlaybackSyncEvent[];
          action_key?: string;
        };
        const key = data.action_key || data.events?.[0]?.action_key || "";
        return { synced: true, action_keys: [key] } as unknown as never;
      },
    );

    // First reconnect: transmits the queued event
    await synchronizeProgress(owner, lessonId);
    expect(serverCalls).toBe(1);
    expect(readSyncQueue(owner, lessonId)).toHaveLength(0);

    // Second reconnect: queue is already empty, no duplicate action sent
    await synchronizeProgress(owner, lessonId);
    expect(serverCalls).toBe(1);
  });

  it("clears the device queue on logout", () => {
    const event: PlaybackSyncEvent = {
      action_key: "act-logout",
      lesson_id: lessonId,
      version: 1,
      slide_id: "sky-1",
      time_ms: 500,
      occurred_at: new Date().toISOString(),
    };
    enqueueSyncEvent(owner, event);
    expect(readSyncQueue(owner, lessonId)).toHaveLength(1);

    clearSyncQueue(owner);
    expect(readSyncQueue(owner, lessonId)).toHaveLength(0);
  });

  it("emits slide_viewed with offline=true and never emits lesson_completed", () => {
    trackPlayer("slide_viewed", {
      lesson_id: lessonId,
      slide_id: "sky-1",
      index: 0,
      watch_ms: 3000,
      offline: true,
    });

    expect(posthog.capture).toHaveBeenCalledWith("slide_viewed", {
      distinct_id: owner,
      source: "client",
      platform: "web",
      app_version: "0.1.0",
      lesson_id: lessonId,
      slide_id: "sky-1",
      index: 0,
      watch_ms: 3000,
      offline: true,
    });
    expect(posthog.capture).not.toHaveBeenCalledWith(
      "lesson_completed",
      expect.anything(),
    );
  });
});

it("drops acknowledged and permanently rejected events while retaining transient failures", async () => {
  for (const action_key of ["accepted", "invalid", "retry"])
    enqueueSyncEvent(owner, {
      action_key,
      lesson_id: lessonId,
      version: 1,
      slide_id: "sky-1",
      time_ms: 0,
      occurred_at: new Date().toISOString(),
    });
  vi.spyOn(session, "request").mockResolvedValueOnce({
    synced: true,
    acked_keys: ["accepted"],
    rejected_keys: ["invalid"],
  });
  await synchronizeProgress(owner, lessonId);
  expect(readSyncQueue(owner, lessonId).map((e) => e.action_key)).toEqual([
    "retry",
  ]);
});

it("drains a large backlog in bounded batches and discards an oversized corrupt event", async () => {
  enqueueSyncEvent(owner, {
    action_key: "oversized",
    lesson_id: lessonId,
    version: 1,
    slide_id: "x".repeat(70000),
    time_ms: 0,
    occurred_at: new Date().toISOString(),
  });
  for (let i = 0; i < 400; i++)
    enqueueSyncEvent(owner, {
      action_key: `event-${i}`,
      lesson_id: lessonId,
      version: 1,
      slide_id: "sky-1",
      time_ms: 0,
      occurred_at: new Date().toISOString(),
    });
  const request = vi
    .spyOn(session, "request")
    .mockImplementation(async (_path, _method, body) => {
      expect(
        new TextEncoder().encode(JSON.stringify(body)).length,
      ).toBeLessThan(65536);
      const events = (body as { events: PlaybackSyncEvent[] }).events;
      expect(events.some((e) => e.action_key === "oversized")).toBe(false);
      return {
        synced: true,
        acked_keys: events.map((e) => e.action_key),
        rejected_keys: [],
      } as never;
    });
  await synchronizeProgress(owner, lessonId);
  expect(request.mock.calls.length).toBeGreaterThan(1);
  expect(readSyncQueue(owner, lessonId)).toEqual([]);
});
