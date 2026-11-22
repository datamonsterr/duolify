import { beforeEach, expect, it, vi } from "vitest";
import { configureAnalytics, trackOnboarding } from "../../lib/analytics";
import { posthog } from "../../lib/posthog";
vi.mock("../../lib/posthog", () => ({
  posthog: {
    capture: vi.fn(),
    identify: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_out_capturing: vi.fn(),
    opt_in_capturing: vi.fn(),
  },
}));
beforeEach(() => {
  vi.clearAllMocks();
  configureAnalytics({
    learnerId: "11111111-1111-4111-8111-111111111111",
    optedOut: false,
    platform: "web",
  });
});
it("emits only catalogued enum properties and common identity", () => {
  trackOnboarding({ event: "onboarding_path_chosen", path: "topic" });
  expect(posthog.capture).toHaveBeenCalledWith(
    "onboarding_path_chosen",
    expect.objectContaining({
      path: "topic",
      source: "client",
      platform: "web",
      distinct_id: "11111111-1111-4111-8111-111111111111",
    }),
  );
  vi.mocked(posthog.capture).mockClear();
  trackOnboarding({
    event: "onboarding_tour_step_viewed",
    step: "private text",
  });
  trackOnboarding({
    event: "onboarding_path_chosen",
    path: "private text",
  } as never);
  expect(posthog.capture).not.toHaveBeenCalled();
});
it("honors account consent and SDK opt-out", () => {
  configureAnalytics({
    learnerId: "11111111-1111-4111-8111-111111111111",
    optedOut: true,
    platform: "web",
  });
  trackOnboarding({ event: "onboarding_welcome_shown" });
  expect(posthog.capture).not.toHaveBeenCalled();
});

it("keeps the emitting tier separate from the tour entry point", () => {
  trackOnboarding({
    event: "onboarding_tour_started",
    entry_point: "settings",
  });
  expect(posthog.capture).toHaveBeenCalledWith(
    "onboarding_tour_started",
    expect.objectContaining({ source: "client", entry_point: "settings" }),
  );
});

it("does not let event properties replace common identity", () => {
  trackOnboarding({
    event: "onboarding_tour_started",
    entry_point: "first_run",
    source: "api",
    distinct_id: "other",
  } as never);
  expect(posthog.capture).toHaveBeenCalledWith(
    "onboarding_tour_started",
    expect.objectContaining({
      source: "client",
      distinct_id: "11111111-1111-4111-8111-111111111111",
      entry_point: "first_run",
    }),
  );
});
