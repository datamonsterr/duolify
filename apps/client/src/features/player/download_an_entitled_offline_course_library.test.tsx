import { describe, it, expect, beforeEach, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { session } from "../accounts/session";
import { clearLessonCache, readCachedLesson } from "./cache";
import {
  downloadCourseLibrary,
  getDownloadedCourses,
  removeCourseFromStorage,
  type CourseDownloadProgress,
  type CourseDownloadResponse,
} from "./library";
import type { Manifest } from "./manifest";
import { configureAnalytics } from "../../lib/analytics";

describe("download an entitled offline course library", () => {
  const learnerId = "11111111-1111-4111-8111-111111111111";
  const entries = new Map<string, Response>();

  beforeEach(async () => {
    vi.restoreAllMocks();
    entries.clear();
    vi.stubGlobal("crypto", webcrypto);
    vi.stubGlobal("caches", {
      open: async () => ({
        match: async (key: string) => entries.get(String(key))?.clone(),
        put: async (key: string, response: Response) => {
          entries.set(String(key), response.clone());
        },
        keys: async () => [...entries.keys()].map((url) => new Request(url)),
        delete: async (key: string | Request) =>
          entries.delete(typeof key === "string" ? key : key.url),
      }),
      keys: async () => ["duolify-player-v1"],
      delete: async () => {
        entries.clear();
        return true;
      },
    });
    await clearLessonCache();
    localStorage.clear();
    sessionStorage.clear();
  });

  it("downloads unlocked lessons in course with progress tracking and storage index", async () => {
    configureAnalytics({
      learnerId,
      optedOut: false,
      platform: "web",
      appVersion: "0.1.0",
    });

    vi.spyOn(session, "current").mockReturnValue({
      id: learnerId,
      display_name: "Test Learner",
      locale: "en",
      time_zone: "UTC",
      background: "",
      analytics_opt_out: false,
    });

    const fakeManifest1 = {
      lesson_id: "lesson-1",
      course_id: "course-1",
      title: "Lesson 1",
      version: 1,
      asset_origin: "https://assets.example.test",
      slides: [
        {
          slide_id: "s1",
          duration_ms: 3000,
          html_url: "/v1/lessons/lesson-1/slides/s1",
          html_sha256:
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          assets: [],
          asset_sha256: {},
          checkpoints: [],
          narration_status: "disabled",
          narration_text: "Intro",
          explanation: "Intro explanation",
        },
      ],
    };

    const fakeManifest2 = {
      lesson_id: "lesson-2",
      course_id: "course-1",
      title: "Lesson 2",
      version: 1,
      asset_origin: "https://assets.example.test",
      slides: [
        {
          slide_id: "s2",
          duration_ms: 3000,
          html_url: "/v1/lessons/lesson-2/slides/s2",
          html_sha256:
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          assets: [],
          asset_sha256: {},
          checkpoints: [],
          narration_status: "disabled",
          narration_text: "Deep",
          explanation: "Deep explanation",
        },
      ],
    };

    // Mock API requests
    vi.spyOn(session, "request").mockImplementation(async (path: string) => {
      if (path === "/v1/courses/course-1/download") {
        return {
          course_id: "course-1",
          title: "Astronomy",
          entitled: true,
          lessons: [
            {
              lesson_id: "lesson-1",
              unit_id: "u1",
              title: "Lesson 1",
              position: 0,
              status: "ready",
              unlocked: true,
              downloadable: true,
              manifest_url: "/v1/lessons/lesson-1/manifest",
            },
            {
              lesson_id: "lesson-2",
              unit_id: "u1",
              title: "Lesson 2",
              position: 1,
              status: "ready",
              unlocked: true,
              downloadable: true,
              manifest_url: "/v1/lessons/lesson-2/manifest",
            },
          ],
        } as unknown as CourseDownloadResponse;
      }
      if (path === "/v1/lessons/lesson-1/manifest")
        return fakeManifest1 as unknown as Manifest;
      if (path === "/v1/lessons/lesson-2/manifest")
        return fakeManifest2 as unknown as Manifest;
      if (path.includes("/slides/"))
        return { html: "", assets: [] } as unknown as {
          html: string;
          assets: unknown[];
        };
      throw new Error(`Unhandled request path: ${path}`);
    });

    const progressReports: CourseDownloadProgress[] = [];
    const result = await downloadCourseLibrary("course-1", (p) =>
      progressReports.push({ ...p }),
    );

    expect(result.success).toBe(true);
    expect(result.downloadedCount).toBe(2);

    // Verify progress sequence was emitted
    expect(progressReports.length).toBeGreaterThanOrEqual(2);
    const lastProgress = progressReports[progressReports.length - 1];
    expect(lastProgress.completed).toBe(2);
    expect(lastProgress.total).toBe(2);

    // Verify lessons are in cache
    const cached1 = await readCachedLesson(learnerId, "lesson-1");
    expect(cached1).not.toBeNull();
    expect(cached1?.manifest.lesson_id).toBe("lesson-1");

    const cached2 = await readCachedLesson(learnerId, "lesson-2");
    expect(cached2).not.toBeNull();
    expect(cached2?.manifest.lesson_id).toBe("lesson-2");

    // Verify course library index tracks download
    const downloaded = getDownloadedCourses(learnerId);
    expect(downloaded["course-1"]).toBeDefined();
    expect(downloaded["course-1"].lessonIds).toContain("lesson-1");
    expect(downloaded["course-1"].lessonIds).toContain("lesson-2");

    // Storage controls can remove the course from offline cache
    await removeCourseFromStorage(learnerId, "course-1");
    const updated = getDownloadedCourses(learnerId);
    expect(updated["course-1"]).toBeUndefined();
    // Normal playback returns to the five-lesson bound after a successful bulk download.
    const { downloadLesson } = await import("./cache");
    for (let i = 0; i < 6; i++)
      await downloadLesson(
        {
          ...fakeManifest1,
          lesson_id: `normal-${i}`,
          slides: fakeManifest1.slides.map((s) => ({
            ...s,
            html_url: `/v1/lessons/normal-${i}/slides/${s.slide_id}`,
          })),
        } as Manifest,
        learnerId,
      );
    expect(entries.size).toBe(5);
  });

  it("refuses new bulk download on unentitled/expired subscription without erasing existing downloads", async () => {
    vi.spyOn(session, "current").mockReturnValue({
      id: learnerId,
      display_name: "Test Learner",
      locale: "en",
      time_zone: "UTC",
      background: "",
      analytics_opt_out: false,
    });

    const existingManifest = {
      lesson_id: "lesson-1",
      course_id: "course-1",
      title: "Lesson 1",
      version: 1,
      asset_origin: "https://assets.example.test",
      slides: [
        {
          slide_id: "s1",
          duration_ms: 3000,
          html_url: "/v1/lessons/lesson-1/slides/s1",
          html_sha256:
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          assets: [],
          asset_sha256: {},
          checkpoints: [],
          narration_status: "disabled",
          narration_text: "Intro",
          explanation: "Intro explanation",
        },
      ],
    };
    vi.spyOn(session, "request").mockImplementation(async (path: string) => {
      if (path === "/v1/lessons/lesson-1/manifest")
        return existingManifest as unknown as Manifest;
      if (path.includes("/slides/"))
        return { html: "", assets: [] } as unknown as {
          html: string;
          assets: unknown[];
        };
      throw new Error("Unhandled");
    });

    // Seed existing cached lesson
    const { downloadLesson } = await import("./cache");
    await downloadLesson(existingManifest as unknown as Manifest, learnerId);
    expect(await readCachedLesson(learnerId, "lesson-1")).not.toBeNull();

    // Now subscription is expired: API returns 403 subscription_required
    vi.spyOn(session, "request").mockRejectedValueOnce(
      Object.assign(new Error("Subscription required"), {
        status: 403,
        error: "subscription_required",
      }),
    );

    const result = await downloadCourseLibrary("course-1");
    expect(result.success).toBe(false);
    expect(result.error).toBe("subscription_required");

    // Existing cached lesson is NOT erased and remains playable offline
    const cached = await readCachedLesson(learnerId, "lesson-1");
    expect(cached).not.toBeNull();
    expect(cached?.manifest.lesson_id).toBe("lesson-1");
    // Failed bulk downloads restore the same bounds.
    for (let i = 0; i < 6; i++)
      await downloadLesson(
        {
          ...existingManifest,
          lesson_id: `normal-${i}`,
          slides: existingManifest.slides.map((s) => ({
            ...s,
            html_url: `/v1/lessons/normal-${i}/slides/${s.slide_id}`,
          })),
        } as Manifest,
        learnerId,
      );
    expect(entries.size).toBe(5);
  });
});

it("never merges another owner's download index without an owner", () => {
  localStorage.setItem(
    "duolify:download-index:alice",
    JSON.stringify({ private: { courseId: "private" } }),
  );
  localStorage.setItem(
    "duolify:download-index:bob",
    JSON.stringify({ own: { courseId: "own" } }),
  );
  expect(getDownloadedCourses("bob")).toEqual({ own: { courseId: "own" } });
  expect(getDownloadedCourses("")).toEqual({});
  localStorage.clear();
});

it.each([401, 503])(
  "preserves HTTP %s for the authenticated home listing",
  async (status) => {
    const { api } = await import("../../lib/api");
    const { AuthError } = await import("../accounts/session");
    vi.spyOn(session, "request").mockRejectedValue(
      new AuthError("denied", status),
    );
    await expect(
      api.courses({ authenticatedOnly: true }),
    ).rejects.toMatchObject({ status });
    vi.restoreAllMocks();
  },
);
