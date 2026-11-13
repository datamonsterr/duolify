import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LearningAnalytics } from "./LearningAnalytics";
import { StreakPanel } from "./Streak";
import { authRequest } from "../accounts/session";
import {
  configureUiAnalytics,
  trackScreen,
  trackLearningAnalyticsControl,
} from "../../lib/analytics";
import { screenForPath } from "../shell/navigation";

vi.mock("../accounts/session", () => ({ authRequest: vi.fn() }));

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

const sampleAnalyticsData = {
  plan_id: "plus",
  plan_name: "Plus",
  entitlement_version: "usd-quotes-2026-11-09",
  time_zone: "Asia/Ho_Chi_Minh",
  local_date: "2026-11-13",
  as_of: "2026-11-13T13:30:00Z",
  custom_export_allowed: false,
  summary: {
    total_xp: 320,
    total_lessons_completed: 16,
    perfect_completions: 8,
    total_duration_ms: 960000,
    current_streak: 5,
    longest_streak: 14,
    freezes_held: 1,
    freezes_used: 1,
  },
  course_breakdown: [
    {
      course_id: "c1",
      title: "Astronomy Basics",
      completed_lessons: 10,
      total_xp: 200,
      perfect_completions: 5,
      total_duration_ms: 600000,
      last_completed_at: "2026-11-12T10:00:00Z",
    },
    {
      course_id: "c2",
      title: "Plant Care",
      completed_lessons: 6,
      total_xp: 120,
      perfect_completions: 3,
      total_duration_ms: 360000,
      last_completed_at: "2026-11-13T08:00:00Z",
    },
  ],
  daily_xp_breakdown: [
    {
      local_date: "2026-11-13",
      xp: 40,
      lessons_completed: 2,
      daily_goal_met: true,
    },
    {
      local_date: "2026-11-12",
      xp: 30,
      lessons_completed: 2,
      daily_goal_met: true,
    },
  ],
  streak_breakdown: {
    current_streak: 5,
    longest_streak: 14,
    freezes_held: 1,
    freezes_used: 1,
    recent_protections: [
      {
        protected_on: "2026-11-10",
        remaining: 1,
        streak_length: 5,
      },
    ],
  },
  completion_breakdown: {
    total_completions: 16,
    perfect_completions: 8,
    total_duration_ms: 960000,
    average_duration_ms: 60000,
    first_completed_at: "2026-11-01T12:00:00Z",
    last_completed_at: "2026-11-13T08:00:00Z",
  },
};

describe("learning analytics component", () => {
  beforeEach(() => {
    vi.mocked(authRequest).mockReset();
    posthog.capture.mockReset();
    // mock URL.createObjectURL and revokeObjectURL for export downloads
    window.URL.createObjectURL = vi.fn(() => "blob:test-url");
    window.URL.revokeObjectURL = vi.fn();
  });

  it("renders advanced learning analytics breakdowns for Plus subscriber", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce(sampleAnalyticsData);

    render(<LearningAnalytics />);

    expect(screen.getByText(/loading learning analytics/i)).toBeInTheDocument();

    expect(await screen.findByText("Astronomy Basics")).toBeInTheDocument();
    expect(screen.getByText("Plant Care")).toBeInTheDocument();
    expect(screen.getByText("320")).toBeInTheDocument(); // total xp
    expect(screen.getByText("16")).toBeInTheDocument(); // total lessons
    expect(screen.getAllByText("5").length).toBeGreaterThanOrEqual(1); // streak or perfect completions

    // Plus has history export enabled
    expect(
      screen.getByRole("button", { name: /export learning history/i }),
    ).toBeInTheDocument();

    // Plus does not have custom dimensions export enabled
    // Plus has "No upsell banners": locked custom export is neutral, no upgrade call to action.
    expect(
      screen.getByText(/custom export is part of pro/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/upgrade/i)).not.toBeInTheDocument();
  });

  it("renders custom analytics dimensions export for Pro subscriber", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce({
      ...sampleAnalyticsData,
      plan_id: "pro",
      plan_name: "Pro",
      custom_export_allowed: true,
    });

    render(<LearningAnalytics />);

    expect(
      await screen.findByRole("button", { name: /export custom analytics/i }),
    ).toBeInTheDocument();

    // Verify dimension checkboxes exist
    expect(screen.getByLabelText(/courses/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/daily xp/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/streaks/i)).toBeInTheDocument();
  });

  it("renders paywall/upgrade state when learner is on Free tier", async () => {
    vi.mocked(authRequest).mockRejectedValueOnce({
      status: 403,
      message:
        "Advanced learning analytics requires an active Plus or Pro subscription.",
      details: { code: "subscription_required" },
    });

    render(<LearningAnalytics />);

    expect(
      await screen.findByText(/plus or pro subscription required/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /view plans/i }),
    ).toBeInTheDocument();
  });

  it("renders expired notice when subscription is expired", async () => {
    vi.mocked(authRequest).mockRejectedValueOnce({
      status: 403,
      message: "Subscription expired. Renew to access learning analytics.",
      details: { code: "subscription_expired" },
    });

    render(<LearningAnalytics />);

    expect(
      await screen.findByRole("heading", { name: /subscription expired/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /renew plan/i }),
    ).toBeInTheDocument();
  });

  it("handles offline error and retries", async () => {
    vi.mocked(authRequest)
      .mockRejectedValueOnce(
        new Error("Learning analytics unavailable. Retry."),
      )
      .mockResolvedValueOnce(sampleAnalyticsData);

    render(<LearningAnalytics />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /learning analytics unavailable/i,
    );
    const retryBtn = screen.getByRole("button", { name: /retry/i });
    fireEvent.click(retryBtn);

    expect(await screen.findByText("Astronomy Basics")).toBeInTheDocument();
  });

  it("emits ui_control_activated when export is triggered", async () => {
    vi.mocked(authRequest)
      .mockResolvedValueOnce(sampleAnalyticsData)
      .mockResolvedValueOnce({
        schema_version: 1,
        exported_at: "2026-11-13T13:30:00Z",
        history: [],
      });

    const context = {
      enabled: true,
      learnerId: "7d3d37e8-4c06-4a0d-b6dc-0d0f3d2ea111",
      platform: "web" as const,
      appVersion: "0.1.0",
    };
    configureUiAnalytics(context);

    render(<LearningAnalytics />);
    const exportBtn = await screen.findByRole("button", {
      name: /export learning history/i,
    });
    fireEvent.click(exportBtn);

    await waitFor(() => {
      expect(authRequest).toHaveBeenCalledWith(
        "/v1/gamification/export/history",
        undefined,
        undefined,
        "GET",
      );
    });

    expect(posthog.capture).toHaveBeenCalledWith(
      "ui_control_activated",
      expect.objectContaining({
        distinct_id: context.learnerId,
        surface: "learning_analytics",
        control_id: "export_history",
      }),
    );
  });
});

describe("learning analytics states", () => {
  beforeEach(() => {
    vi.mocked(authRequest).mockReset();
    posthog.capture.mockReset();
  });

  it("does not emit its own ui_screen_viewed (the shell emits it once per route)", async () => {
    configureUiAnalytics({
      enabled: true,
      learnerId: "7d3d37e8-4c06-4a0d-b6dc-0d0f3d2ea111",
      platform: "web",
      appVersion: "0.1.0",
    });
    vi.mocked(authRequest).mockResolvedValueOnce(sampleAnalyticsData);
    render(<LearningAnalytics />);
    await screen.findByText("Astronomy Basics");
    expect(posthog.capture).not.toHaveBeenCalledWith(
      "ui_screen_viewed",
      expect.anything(),
    );
    configureUiAnalytics(null);
  });

  it("keeps upsell copy away from plans with No upsell banners but still offers history export", async () => {
    vi.mocked(authRequest).mockRejectedValueOnce({
      status: 403,
      message:
        "Advanced learning analytics requires an active Plus or Pro subscription.",
      details: { code: "subscription_required", upsell_suppressed: true },
    });
    render(<LearningAnalytics />);
    expect(
      await screen.findByText(/not included in your plan/i),
    ).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /view plans/i })).toBeNull();
    expect(
      screen.getByRole("button", { name: /export learning history/i }),
    ).toBeInTheDocument();
  });

  it("offers plans but no export to a Learner without a plan", async () => {
    vi.mocked(authRequest).mockRejectedValueOnce({
      status: 403,
      message: "x",
      details: { code: "subscription_required", upsell_suppressed: false },
    });
    render(<LearningAnalytics />);
    expect(
      await screen.findByRole("link", { name: /view plans/i }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /export learning history/i }),
    ).toBeNull();
  });

  it("shows streak and completion breakdowns", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce(sampleAnalyticsData);
    render(<LearningAnalytics />);
    expect(
      await screen.findByRole("heading", { name: /completion breakdown/i }),
    ).toBeInTheDocument();
    expect(screen.getByText(/average lesson time/i)).toBeInTheDocument();
    expect(screen.getByText("2026-11-10")).toBeInTheDocument();
  });

  it("is reachable from the streak page", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce({
      current_days: 1,
      longest_days: 1,
      today_done: true,
      local_date: "2026-11-09",
      time_zone: "UTC",
      as_of: "2026-11-09T08:42:00Z",
      rollover_pending: false,
      month: "2026-11",
      calendar: [],
    });
    render(<StreakPanel />);
    expect(
      await screen.findByRole("link", { name: /analytics and exports/i }),
    ).toHaveAttribute("href", "/analytics");
  });

  it("says when the device is offline", async () => {
    vi.mocked(authRequest).mockRejectedValueOnce(
      new TypeError("Failed to fetch"),
    );
    render(<LearningAnalytics />);
    expect(await screen.findByRole("alert")).toHaveTextContent(/offline/i);
  });

  it("downloads only server-allowlisted dimensions in canonical order", async () => {
    vi.mocked(authRequest)
      .mockResolvedValueOnce({
        ...sampleAnalyticsData,
        custom_export_allowed: true,
      })
      .mockResolvedValueOnce({ schema_version: 1 });
    window.URL.createObjectURL = vi.fn(() => "blob:x");
    window.URL.revokeObjectURL = vi.fn();
    render(<LearningAnalytics />);
    fireEvent.click(await screen.findByLabelText(/streaks/i));
    fireEvent.click(
      screen.getByRole("button", { name: /export custom analytics/i }),
    );
    await waitFor(() =>
      expect(authRequest).toHaveBeenLastCalledWith(
        "/v1/gamification/export/analytics",
        { dimensions: ["course", "daily", "streak"] },
        undefined,
        "POST",
      ),
    );
  });
});

describe("learning analytics telemetry", () => {
  const context = {
    enabled: true,
    learnerId: "7d3d37e8-4c06-4a0d-b6dc-0d0f3d2ea111",
    platform: "desktop" as const,
    appVersion: "0.1.0",
  };

  afterEach(() => {
    configureUiAnalytics(null);
    vi.clearAllMocks();
  });

  it("maps /analytics to learning_analytics screen and delivers telemetry", () => {
    expect(screenForPath("/analytics")).toBe("learning_analytics");
    configureUiAnalytics(context);
    trackScreen("learning_analytics");
    expect(posthog.capture).toHaveBeenCalledWith(
      "ui_screen_viewed",
      expect.objectContaining({
        distinct_id: context.learnerId,
        screen_id: "learning_analytics",
      }),
    );
  });

  it("suppresses telemetry when opted out", () => {
    configureUiAnalytics({ ...context, enabled: false });
    trackScreen("learning_analytics");
    trackLearningAnalyticsControl("export_history");
    expect(posthog.capture).not.toHaveBeenCalled();
  });
});
