import { afterEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "./queryClient";
import {
  invalidateCourseList,
  invalidateCourseProgress,
  registerCourseQueryClient,
} from "./courseQueries";

const request = vi.hoisted(() => vi.fn());
vi.mock("../features/accounts/session", () => ({
  AuthError: class AuthError extends Error {},
  session: { current: () => ({ id: "owner" }), request },
}));

import { savePosition } from "../features/player/position";
import { api } from "./api";

function seeded() {
  const client = createQueryClient();
  registerCourseQueryClient(client);
  client.setQueryData(["course", "c1", "owner"], { id: "c1" });
  client.setQueryData(["course", "c2", "owner"], { id: "c2" });
  client.setQueryData(["courses", "owner"], []);
  client.setQueryData(["courses"], []);
  client.setQueryData(["wallet"], {});
  return client;
}
const stale = (client: ReturnType<typeof seeded>, key: unknown[]) =>
  client.getQueryState(key)?.isInvalidated;

afterEach(() => {
  registerCourseQueryClient(null);
  request.mockReset();
});

describe("course query invalidation", () => {
  it("stays fresh under the shared stale time until something invalidates it", () => {
    const client = seeded();
    expect(stale(client, ["course", "c1", "owner"])).toBe(false);
    expect(
      client.getQueryCache().find({ queryKey: ["course", "c1", "owner"] }),
    ).toBeDefined();
    expect(client.getDefaultOptions().queries?.staleTime).toBe(30_000);
  });

  it("progress marks every course tree and both course-list keys, nothing else", () => {
    const client = seeded();
    invalidateCourseProgress();
    expect(stale(client, ["course", "c1", "owner"])).toBe(true);
    expect(stale(client, ["course", "c2", "owner"])).toBe(true);
    expect(stale(client, ["courses", "owner"])).toBe(true);
    expect(stale(client, ["courses"])).toBe(true);
    expect(stale(client, ["wallet"])).toBe(false);
  });

  it("a list change leaves the trees alone", () => {
    const client = seeded();
    invalidateCourseList();
    expect(stale(client, ["courses", "owner"])).toBe(true);
    expect(stale(client, ["course", "c1", "owner"])).toBe(false);
  });

  it("is a no-op before a client is registered", () => {
    registerCourseQueryClient(null);
    expect(() => invalidateCourseProgress()).not.toThrow();
  });

  it("saving lesson progress invalidates the course path and list", async () => {
    const client = seeded();
    const position = { version: 1, slide_id: "s1", time_ms: 0 };
    request.mockResolvedValue(position);
    await savePosition("owner", "lesson-1", position);
    expect(stale(client, ["course", "c1", "owner"])).toBe(true);
    expect(stale(client, ["courses", "owner"])).toBe(true);
  });

  it("deleting a course invalidates; switching one refreshes only the list", async () => {
    const client = seeded();
    request.mockResolvedValue({ active_course_id: "c2" });
    await api.selectCourse("c2");
    expect(stale(client, ["courses", "owner"])).toBe(true);
    expect(stale(client, ["course", "c1", "owner"])).toBe(false);
    request.mockResolvedValue({ deleted: true });
    await api.deleteCourse("c1");
    expect(stale(client, ["course", "c1", "owner"])).toBe(true);
  });
});
