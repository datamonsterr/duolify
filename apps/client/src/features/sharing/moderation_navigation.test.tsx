import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import type { ReactElement } from "react";
import { session } from "../accounts/session";
import { ModerationNoticesLink } from "./ModerationNoticesLink";
import { CourseModerationBanner } from "./CourseModerationBanner";
import {
  clearNoticesCache,
  getCachedNotices,
  refreshNotices,
  type Notice,
} from "./moderation";

vi.mock("../accounts/session", () => {
  let currentLearner: { id: string } | null = { id: "author-1" };
  const listeners = new Set<() => void>();
  return {
    session: {
      request: vi.fn(),
      current: vi.fn(() => currentLearner),
      subscribe: vi.fn((listener: () => void) => {
        listeners.add(listener);
        return () => listeners.delete(listener);
      }),
      __setLearner: (learner: { id: string } | null) => {
        currentLearner = learner;
        listeners.forEach((fn) => fn());
      },
    },
  };
});

const request = vi.mocked(session.request);
const setLearner = (
  session as unknown as {
    __setLearner: (l: { id: string } | null) => void;
  }
).__setLearner;

function renderWithRouter(ui: ReactElement, initialEntry = "/") {
  const root = createRootRoute({ component: () => ui });
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  });
  return render(<RouterProvider router={router} />);
}

describe("moderation navigation entry points and course notice banner", () => {
  beforeEach(() => {
    setLearner({ id: "author-1" });
    clearNoticesCache();
    request.mockReset();
  });

  const attentionNotice: Notice = {
    id: "act-1",
    action: "delist",
    reason_code: "misleading",
    reason: "The title promises more than the lessons deliver.",
    target_type: "course",
    course_id: "course-123",
    title: "Astronomy basics",
    severe: false,
    created_at: "2026-11-02T00:00:00Z",
    can_appeal: true,
  };

  const openAppealNotice: Notice = {
    id: "act-2",
    action: "warn",
    reason_code: "inappropriate",
    reason: "Course cover contains unsuitable wording.",
    target_type: "course",
    course_id: "course-456",
    title: "Another course",
    severe: false,
    created_at: "2026-11-03T00:00:00Z",
    can_appeal: false,
    appeal: { status: "open" },
  };

  const decidedNotice: Notice = {
    id: "act-3",
    action: "warn",
    reason_code: "spam",
    reason: "Duplicate course content.",
    target_type: "course",
    course_id: "course-123",
    title: "Astronomy basics",
    severe: false,
    created_at: "2026-11-01T00:00:00Z",
    can_appeal: false,
    appeal: { status: "upheld" },
  };

  it("renders a link with a count badge and accessible aria-label when user has notices needing attention", async () => {
    request.mockResolvedValueOnce([
      attentionNotice,
      openAppealNotice,
      decidedNotice,
    ]);
    renderWithRouter(<ModerationNoticesLink />);

    const link = await screen.findByRole("link", {
      name: "Moderation notices (2)",
    });
    expect(link).toHaveAttribute("href", "/moderation/notices");
    expect(link).toHaveAttribute("aria-label", "Moderation notices (2)");

    const badge = screen.getByText("2");
    expect(badge).toHaveClass("badge");
  });

  it("renders the link without a badge and with plain aria-label when all notices are decided", async () => {
    request.mockResolvedValueOnce([decidedNotice]);
    renderWithRouter(<ModerationNoticesLink />);

    const link = await screen.findByRole("link", {
      name: "Moderation notices",
    });
    expect(link).toHaveAttribute("href", "/moderation/notices");
    expect(link).toHaveAttribute("aria-label", "Moderation notices");
    expect(screen.queryByText(/[0-9]+/)).toBeNull();
  });

  it("is hidden when the user has zero notices", async () => {
    request.mockResolvedValueOnce([]);
    const { container } = renderWithRouter(<ModerationNoticesLink />);

    await waitFor(() => {
      expect(
        screen.queryByRole("link", { name: /moderation notices/i }),
      ).toBeNull();
    });
    expect(container.querySelector("a")).toBeNull();
  });

  it("renders attention copy in course banner when a notice on this course needs attention", async () => {
    request.mockResolvedValueOnce([attentionNotice]);
    renderWithRouter(<CourseModerationBanner courseId="course-123" />);

    const banner = await screen.findByRole("status");
    expect(banner).toHaveClass("ban");
    expect(banner).toHaveClass("warn");
    expect(banner).toHaveClass("mod-banner");
    expect(banner).toHaveTextContent(
      "This course has a moderation notice that needs your attention.",
    );

    const link = screen.getByRole("link", {
      name: "View moderation notices",
    });
    expect(link).toHaveAttribute("href", "/moderation/notices");
    expect(link).toHaveClass("mod-banner-link");
  });

  it("renders generic copy in course banner without claiming open when all notices on the course are decided", async () => {
    request.mockResolvedValueOnce([decidedNotice]);
    renderWithRouter(<CourseModerationBanner courseId="course-123" />);

    const banner = await screen.findByRole("status");
    expect(banner).toHaveTextContent("This course has a moderation notice.");
    expect(banner).not.toHaveTextContent("needs your attention");
    expect(banner).not.toHaveTextContent("open");

    const link = screen.getByRole("link", {
      name: "View moderation notices",
    });
    expect(link).toHaveAttribute("href", "/moderation/notices");
  });

  it("does not render a banner on a course without notices", async () => {
    request.mockResolvedValueOnce([attentionNotice]);
    const { container } = renderWithRouter(
      <CourseModerationBanner courseId="course-999" />,
    );

    await waitFor(() => {
      expect(screen.queryByRole("status")).toBeNull();
    });
    expect(container.querySelector("[role='status']")).toBeNull();
  });

  it("clears cached notices on sign-out and account switch so a different user never sees prior notices", async () => {
    request.mockResolvedValueOnce([attentionNotice]);
    const author1Notices = await refreshNotices();
    expect(author1Notices).toHaveLength(1);
    expect(getCachedNotices()).toHaveLength(1);

    // Sign out: session.current() returns null
    setLearner(null);
    const signedOutNotices = await refreshNotices();
    expect(signedOutNotices).toHaveLength(0);
    expect(getCachedNotices()).toHaveLength(0);

    // Switch account to author-2: should fetch author-2 notices, never seeing author-1
    setLearner({ id: "author-2" });
    const author2Notice: Notice = {
      ...decidedNotice,
      id: "act-author2",
      title: "Author 2 course",
    };
    request.mockResolvedValueOnce([author2Notice]);
    const author2Notices = await refreshNotices();
    expect(author2Notices).toHaveLength(1);
    expect(author2Notices[0].id).toBe("act-author2");
    expect(getCachedNotices()[0].id).toBe("act-author2");
  });
});
