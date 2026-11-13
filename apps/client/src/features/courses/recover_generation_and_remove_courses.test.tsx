import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, afterEach } from "vitest";
import { CourseCreate } from "./CourseCreate";
import { CourseCard } from "../design/CourseCard";
import { configureUiAnalytics } from "../../lib/analytics";
import { session } from "../accounts/session";
import type { Course } from "../../lib/api";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

afterEach(() => {
  configureUiAnalytics(null);
  vi.clearAllMocks();
  sessionStorage.clear();
});

describe("Recover generation and retry in CourseCreate", () => {
  const snapshot = {
    draft_id: "draft-e204",
    revision: 2,
    content_hash: "hash-e204",
    outline: [
      {
        id: "u1",
        title: "Recovered Unit",
        lessons: [{ id: "l1", title: "Recovered Lesson", goal: "Learn" }],
      },
    ],
  };

  it("reconnects to existing active job and handles terminal failure with S04 banner and explicit retry", async () => {
    configureUiAnalytics({
      learnerId: "11111111-1111-4111-8111-111111111111",
      platform: "web",
      appVersion: "0.1.0",
      enabled: true,
    });
    vi.spyOn(session, "current").mockReturnValue({
      id: "11111111-1111-4111-8111-111111111111",
      display_name: "Learner",
      locale: "en",
      time_zone: "UTC",
      background: "",
      analytics_opt_out: false,
    });

    const key = `course-creation:11111111-1111-4111-8111-111111111111:${snapshot.draft_id}:${snapshot.revision}`;
    sessionStorage.setItem(key, "job:job-recovery-1");

    let watchCallback:
      | ((c: { id: string; state: string; course_id: string }) => void)
      | undefined;
    const service = {
      quote: vi.fn().mockResolvedValue({
        id: "quote-fresh-2",
        content_ceiling: 55,
        audio_ceiling: 0,
        pricing_version: "fixture-quote",
        expires_at: new Date(Date.now() + 60000).toISOString(),
      }),
      wallet: vi.fn().mockResolvedValue({ purchased: 200, subscription: 0 }),
      accept: vi.fn().mockResolvedValue({}),
      create: vi.fn().mockResolvedValue({
        id: "job-recovery-2",
        state: "enrolled",
        course_id: "course-123",
      }),
      get: vi.fn().mockResolvedValue({
        id: "job-recovery-1",
        state: "generating",
        course_id: "",
      }),
      watch: vi.fn().mockImplementation((_id, onStage) => {
        watchCallback = onStage;
        return new Promise(() => {}); // keep connection open
      }),
    };

    render(
      <CourseCreate snapshot={snapshot} onBack={() => {}} service={service} />,
    );

    // Initial mount with recovery automatically checks or resumes status
    await waitFor(() => {
      expect(service.get).toHaveBeenCalledWith("job-recovery-1");
    });

    // When status is returned, SSE watch is initiated
    await waitFor(() => {
      expect(service.watch).toHaveBeenCalledWith(
        "job-recovery-1",
        expect.any(Function),
        expect.any(AbortSignal),
      );
    });

    // Simulate SSE delivering terminal failure
    watchCallback?.({
      id: "job-recovery-1",
      state: "failed",
      course_id: "",
    });

    // Matches Canvas S04 banner: "Couldn't create it", "released", "Try again"
    await screen.findByText("Couldn't create it");
    expect(
      screen.getByText(/Your 55 reserved credits were released/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/Your edits are saved/i)).toBeInTheDocument();

    const tryAgain = screen.getByRole("button", { name: "Try again" });
    fireEvent.click(tryAgain);

    // Explicit retry requires fresh consent
    expect(service.create).not.toHaveBeenCalledWith(snapshot, "quote-fresh-2");
    fireEvent.click(
      screen.getByRole("button", { name: "Review creation cost" }),
    );
    await screen.findByText(/Reserve up to 55 credits/);

    fireEvent.click(
      screen.getByRole("button", { name: /Create outline · up to/i }),
    );
    await waitFor(() => {
      expect(service.accept).toHaveBeenCalledWith("quote-fresh-2");
      expect(service.create).toHaveBeenCalledWith(snapshot, "quote-fresh-2");
    });
  });
});

describe("CourseCard remove action and accessible confirmation dialog", () => {
  const course: Course = {
    id: "course-delete-test",
    title: "Astronomy for Beginners",
    description: "Explore the solar system.",
    units: [
      {
        id: "u1",
        title: "Unit 1",
        lessons: [
          { id: "l1", title: "Lesson 1", goal: "Goal 1", state: "done" },
        ],
      },
    ],
  };

  it("opens confirmation dialog with 30-day grace explanation and allows cancelling or confirming deletion", async () => {
    const onRemove = vi.fn().mockResolvedValue({ deleted: true });

    render(<CourseCard course={course} onRemove={onRemove} />);

    const removeBtn = screen.getByRole("button", {
      name: /Remove course Astronomy for Beginners/i,
    });
    expect(removeBtn).toBeInTheDocument();

    // Click remove button opens dialog
    fireEvent.click(removeBtn);

    const dialog = screen.getByRole("dialog", { name: /Remove course/i });
    expect(dialog).toBeInTheDocument();
    expect(
      screen.getByText(/permanently purged after 30 days/i),
    ).toBeInTheDocument();

    // Cancel closes dialog
    const cancelBtn = screen.getByRole("button", { name: /Cancel/i });
    fireEvent.click(cancelBtn);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(onRemove).not.toHaveBeenCalled();

    // Reopen and confirm deletion
    fireEvent.click(removeBtn);
    const confirmBtn = screen.getByRole("button", {
      name: /Confirm removal|Delete course/i,
    });
    fireEvent.click(confirmBtn);

    await waitFor(() => {
      expect(onRemove).toHaveBeenCalledWith("course-delete-test");
    });
  });
});
