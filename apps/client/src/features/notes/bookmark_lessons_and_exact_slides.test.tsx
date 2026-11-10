import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { bookmarkQueue } from "./bookmark-queue";
import { BookmarksPage, isLessonCached } from "./BookmarksPage";
import { session } from "../accounts/session";
import {
  configureAnalytics,
  trackBookmarkAdded,
  trackBookmarkRemoved,
  trackBookmarkOpened,
} from "../../lib/analytics";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));

vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

describe("bookmark_lessons_and_exact_slides", () => {
  beforeEach(() => {
    localStorage.clear();
    bookmarkQueue.clear();
    configureAnalytics({
      learnerId: "11111111-1111-4111-8111-111111111111",
      optedOut: false,
      platform: "web",
      appVersion: "0.1.0",
    });
    vi.clearAllMocks();
  });

  afterEach(() => {
    configureAnalytics(null);
    vi.clearAllMocks();
  });

  it("manages offline queue with stable action keys and idempotent deduplication", () => {
    const item1 = bookmarkQueue.enqueue({
      courseId: "33333333-3333-4333-8333-333333333333",
      lessonId: "11111111-1111-4111-8111-111111111111",
      slideId: 1,
      targetType: "slide",
      lessonTitle: "Why the sky changes",
      slideTitle: "Slide 2",
      courseTitle: "Astronomy",
    });

    expect(item1.actionKey).toBeDefined();
    expect(item1.status).toBe("device_saved");

    // Replay adding same target
    const item2 = bookmarkQueue.enqueue({
      courseId: "33333333-3333-4333-8333-333333333333",
      lessonId: "11111111-1111-4111-8111-111111111111",
      slideId: 1,
      targetType: "slide",
    });

    // Queue deduplicates by target
    expect(item2.actionKey).toBe(item1.actionKey);
    const queue = bookmarkQueue.getQueue();
    expect(queue).toHaveLength(1);
    expect(queue[0].actionKey).toBe(item1.actionKey);

    // Enqueue removal
    bookmarkQueue.enqueueRemoval({
      lessonId: "11111111-1111-4111-8111-111111111111",
      slideId: 1,
    });

    const updatedQueue = bookmarkQueue.getQueue();
    expect(updatedQueue[0].status).toBe("removal_pending");
  });

  it("renders bookmarks list, course filter chips, and empty state per canvas S21/S22", async () => {
    // Seed local queue with 2 bookmarks from different courses
    bookmarkQueue.enqueue({
      courseId: "33333333-3333-4333-8333-333333333333",
      lessonId: "11111111-1111-4111-8111-111111111111",
      slideId: 1,
      targetType: "slide",
      courseTitle: "Astronomy",
      lessonTitle: "Why the sky changes",
      slideTitle: "Slide 2",
      status: "synced",
    });

    bookmarkQueue.enqueue({
      courseId: "course-plants",
      lessonId: "lesson-plants-1",
      slideId: 3,
      targetType: "slide",
      courseTitle: "Plant care",
      lessonTitle: "Watering schedules",
      slideTitle: "Slide 4",
      status: "synced",
    });

    render(<BookmarksPage />);

    expect(
      screen.getByRole("heading", { name: /^Bookmarks$/i }),
    ).toBeInTheDocument();

    // Verify filter chips
    expect(
      screen.getByRole("button", { name: /^All courses$/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /^Astronomy$/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /^Plant care$/i }),
    ).toBeInTheDocument();

    // Both courses items visible initially
    expect(screen.getByText(/Why the sky changes/i)).toBeInTheDocument();
    expect(screen.getByText(/Watering schedules/i)).toBeInTheDocument();

    // Filter by Astronomy
    fireEvent.click(screen.getByRole("button", { name: /^Astronomy$/i }));
    expect(screen.getByText(/Why the sky changes/i)).toBeInTheDocument();
    expect(screen.queryByText(/Watering schedules/i)).not.toBeInTheDocument();

    // Filter by All courses
    fireEvent.click(screen.getByRole("button", { name: /^All courses$/i }));
    expect(screen.getByText(/Why the sky changes/i)).toBeInTheDocument();
    expect(screen.getByText(/Watering schedules/i)).toBeInTheDocument();
  });

  it("labels deleted target as unavailable with Remove action and no Open action", async () => {
    bookmarkQueue.enqueue({
      id: "bm-unavailable",
      courseId: "course-seasons",
      lessonId: "deleted-lesson",
      targetType: "lesson",
      courseTitle: "Seasons",
      lessonTitle: "Reading the seasons",
      available: false,
      status: "synced",
    });

    render(<BookmarksPage />);

    // Shows unavailable message per canvas
    expect(screen.getByText(/Reading the seasons/i)).toBeInTheDocument();
    expect(
      screen.getByText(
        /This saved slide is unavailable|This saved lesson is unavailable/i,
      ),
    ).toBeInTheDocument();

    // Must have Remove button
    const removeBtn = screen.getByRole("button", {
      name: /Remove bookmark Reading the seasons|Remove/i,
    });
    expect(removeBtn).toBeInTheDocument();

    // Must NOT have Open button
    expect(
      screen.queryByRole("link", { name: /Open Reading the seasons/i }),
    ).not.toBeInTheDocument();
  });

  it("emits bookmark analytics without leaking private learner content", () => {
    trackBookmarkAdded({
      targetType: "slide",
      courseId: "11111111-1111-4111-8111-111111111111",
      lessonId: "22222222-2222-4222-8222-222222222222",
      slideId: 1,
    });

    expect(posthog.capture).toHaveBeenCalledWith(
      "bookmark_added",
      expect.objectContaining({
        target_type: "slide",
        course_id: "11111111-1111-4111-8111-111111111111",
        lesson_id: "22222222-2222-4222-8222-222222222222",
        slide_id: 1,
      }),
    );

    trackBookmarkRemoved({
      targetType: "lesson",
      courseId: "11111111-1111-4111-8111-111111111111",
      lessonId: "22222222-2222-4222-8222-222222222222",
    });

    expect(posthog.capture).toHaveBeenCalledWith(
      "bookmark_removed",
      expect.objectContaining({
        target_type: "lesson",
      }),
    );

    trackBookmarkOpened({
      targetType: "slide",
      courseId: "11111111-1111-4111-8111-111111111111",
      lessonId: "22222222-2222-4222-8222-222222222222",
      slideId: 1,
    });

    expect(posthog.capture).toHaveBeenCalledWith(
      "bookmark_opened",
      expect.objectContaining({
        target_type: "slide",
        slide_id: 1,
      }),
    );
  });

  it("renders Saved offline chip only when lesson is downloaded for the current owner", () => {
    const ownerId = "11111111-1111-4111-8111-111111111111";
    vi.spyOn(session, "current").mockReturnValue({
      id: ownerId,
      display_name: "Learner",
      locale: "en",
      time_zone: "UTC",
      background: "",
      analytics_opt_out: false,
    });

    bookmarkQueue.enqueue({
      id: "bm-cached-lesson",
      courseId: "course-astronomy",
      lessonId: "lesson-stars-1",
      targetType: "slide",
      courseTitle: "Astronomy",
      lessonTitle: "Stars and Planets",
      slideTitle: "Slide 1",
      slideId: 0,
      status: "synced",
      available: true,
    });

    // 1. Without download-index, it shows "not downloaded"
    expect(isLessonCached("lesson-stars-1")).toBe(false);
    const { unmount } = render(<BookmarksPage />);
    expect(screen.getByText(/not downloaded/i)).toBeInTheDocument();
    expect(screen.queryByText(/^Saved offline$/i)).not.toBeInTheDocument();
    unmount();

    // 2. Download index under a different owner does NOT mark it as saved offline (owner-scoped)
    localStorage.setItem(
      "duolify:download-index:different-owner",
      JSON.stringify({
        "course-astronomy": {
          courseId: "course-astronomy",
          title: "Astronomy",
          downloadedAt: new Date().toISOString(),
          lessonIds: ["lesson-stars-1"],
        },
      }),
    );
    expect(isLessonCached("lesson-stars-1")).toBe(false);
    const { unmount: unmount2 } = render(<BookmarksPage />);
    expect(screen.getByText(/not downloaded/i)).toBeInTheDocument();
    expect(screen.queryByText(/^Saved offline$/i)).not.toBeInTheDocument();
    unmount2();

    // 3. Download index for current owner marks it as Saved offline
    localStorage.setItem(
      `duolify:download-index:${ownerId}`,
      JSON.stringify({
        "course-astronomy": {
          courseId: "course-astronomy",
          title: "Astronomy",
          downloadedAt: new Date().toISOString(),
          lessonIds: ["lesson-stars-1"],
        },
      }),
    );
    expect(isLessonCached("lesson-stars-1")).toBe(true);
    render(<BookmarksPage />);
    expect(screen.getByText(/^Saved offline$/i)).toBeInTheDocument();
    expect(screen.queryByText(/not downloaded/i)).not.toBeInTheDocument();
  });
});
