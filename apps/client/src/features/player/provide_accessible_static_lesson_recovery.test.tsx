import { describe, expect, it, vi, afterEach } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import { isManifest } from "./manifest";
import {
  configureAnalytics,
  trackPlayer,
  trackPlayerRecovery,
} from "../../lib/analytics";
import { session } from "../accounts/session";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { LessonPlayer } from "../lesson-player/LessonPlayer";
import { playerManifest } from "../../mocks/player";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));

afterEach(() => {
  vi.restoreAllMocks();
  configureAnalytics(null);
  sessionStorage.clear();
  vi.clearAllMocks();
});

describe("accessible static recovery manifest contract", () => {
  it("validates explanation field in manifest DTO and rejects oversized explanation", () => {
    const valid = playerManifest();
    (valid.slides[0] as unknown as { explanation: string }).explanation =
      "A complete static instructional explanation of stellar motions.";
    expect(isManifest(valid)).toBe(true);

    const oversized = playerManifest();
    (oversized.slides[0] as unknown as { explanation: string }).explanation =
      "x".repeat(20001);
    expect(isManifest(oversized)).toBe(false);

    const nonString = playerManifest();
    (nonString.slides[0] as unknown as { explanation: number }).explanation =
      12345;
    expect(isManifest(nonString)).toBe(false);
  });

  it("emits lesson_player_error and ui_recovery_selected for asset recovery without private text", () => {
    const owner = "a1111111-1111-4111-8111-111111111111";
    configureAnalytics({
      learnerId: owner,
      optedOut: false,
      platform: "web",
      appVersion: "0.1.0",
    });

    trackPlayer("lesson_player_error", {
      lesson_id: "sky",
      slide_id: "sky-1",
      error_code: "asset_unavailable",
    });

    expect(posthog.capture).toHaveBeenCalledWith("lesson_player_error", {
      distinct_id: owner,
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      lesson_id: "sky",
      slide_id: "sky-1",
      error_code: "asset_unavailable",
    });

    trackPlayerRecovery("retry", "asset_unavailable");
    expect(posthog.capture).toHaveBeenCalledWith("ui_recovery_selected", {
      distinct_id: owner,
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      surface: "lesson_player",
      error_code: "asset_unavailable",
      action: "retry",
    });

    trackPlayerRecovery("exit", "asset_unavailable");
    expect(posthog.capture).toHaveBeenCalledWith("ui_recovery_selected", {
      distinct_id: owner,
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      surface: "lesson_player",
      error_code: "asset_unavailable",
      action: "exit",
    });

    // Opt-out disables telemetry completely
    configureAnalytics({ learnerId: owner, optedOut: true, platform: "web" });
    trackPlayerRecovery("retry", "asset_unavailable");
    expect(posthog.capture).toHaveBeenCalledTimes(3);
  });
});

describe("lesson player missing asset recovery and accessible static UI", () => {
  it("renders canvas missing asset recovery state, disables Next, and never marks completion", async () => {
    const owner = "a1111111-1111-4111-8111-111111111111";
    vi.spyOn(session, "current").mockReturnValue({
      id: owner,
      display_name: "Test Learner",
      locale: "en",
      time_zone: "UTC",
      background: "",
      analytics_opt_out: false,
    });

    const manifest = playerManifest();
    (manifest.slides[0] as unknown as { explanation: string }).explanation =
      "Static explanation for slide one.";

    vi.spyOn(session, "request").mockImplementation(async (path) => {
      if (path.endsWith("/manifest")) return manifest as never;
      if (path.includes("/slides/")) {
        // Simulate missing diagram asset
        const err = new Error("asset_unavailable");
        throw err;
      }
      return null as never;
    });

    configureAnalytics({ learnerId: owner, optedOut: false, platform: "web" });

    const router = createRouter({
      history: createMemoryHistory({ initialEntries: ["/"] }),
      routeTree: createRootRoute({ component: () => <LessonPlayer /> }),
    });

    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    // Assert canvas failure card appears
    await waitFor(() => {
      expect(screen.getByText("This slide didn't load")).toBeInTheDocument();
    });
    expect(
      screen.getByText(/A diagram asset is missing. Your place is saved./),
    ).toBeInTheDocument();

    // Verify recovery controls
    const retryBtn = screen.getByRole("button", { name: "Retry slide" });
    const exitBtn = screen.getByRole("button", { name: "Exit to path" });
    expect(retryBtn).toBeInTheDocument();
    expect(exitBtn).toBeInTheDocument();

    // Verify Next button is disabled and completion is not granted
    const nextBtn = screen.getByRole("button", { name: /Next/ });
    expect(nextBtn).toBeDisabled();

    // Transcript remains accessible
    const transcriptBtn = screen.getByRole("button", { name: "Transcript" });
    expect(transcriptBtn).toBeInTheDocument();

    // Selecting Exit emits ui_recovery_selected
    fireEvent.click(exitBtn);
    expect(posthog.capture).toHaveBeenCalledWith(
      "ui_recovery_selected",
      expect.objectContaining({
        surface: "lesson_player",
        action: "exit",
      }),
    );

    // Selecting Retry emits ui_recovery_selected
    fireEvent.click(retryBtn);
    expect(posthog.capture).toHaveBeenCalledWith(
      "ui_recovery_selected",
      expect.objectContaining({
        surface: "lesson_player",
        action: "retry",
      }),
    );
  });
});
