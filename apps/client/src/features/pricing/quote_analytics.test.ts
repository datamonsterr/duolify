import { beforeEach, expect, it, vi } from "vitest";
import { configureAnalytics, trackQuoteShown } from "../../lib/analytics";
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
  configureAnalytics(null);
  configureAnalytics({
    learnerId: "11111111-1111-4111-8111-111111111111",
    optedOut: false,
    platform: "web",
  });
});

it("captures a quote view once with only the joinable terms", () => {
  const quote = {
    quote_id: "22222222-2222-4222-8222-222222222222",
    feature: "lesson_content",
    pricing_version: "v1",
  };
  trackQuoteShown(quote);
  trackQuoteShown(quote);
  configureAnalytics(null);
  configureAnalytics({
    learnerId: "11111111-1111-4111-8111-111111111111",
    optedOut: false,
    platform: "web",
  });
  trackQuoteShown(quote);
  expect(posthog.capture).toHaveBeenCalledTimes(1);
  expect(posthog.capture).toHaveBeenCalledWith(
    "action_quote_shown",
    expect.objectContaining({
      ...quote,
      source: "client",
      distinct_id: "11111111-1111-4111-8111-111111111111",
    }),
  );
});

it("rejects text and honors opt-out", () => {
  trackQuoteShown({
    quote_id: "a learner's text",
    feature: "private text",
    pricing_version: "v1",
  });
  configureAnalytics({
    learnerId: "11111111-1111-4111-8111-111111111111",
    optedOut: true,
    platform: "web",
  });
  trackQuoteShown({
    quote_id: "33333333-3333-4333-8333-333333333333",
    feature: "preview",
    pricing_version: "v1",
  });
  expect(posthog.capture).not.toHaveBeenCalled();
});
