import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { configureAnalytics } from "../../lib/analytics";
import { posthog } from "../../lib/posthog";
import { QuotePanel } from "./QuotePanel";
import { quoteAPI } from "./quotes";

vi.mock("../../lib/posthog", () => ({
  posthog: {
    capture: vi.fn(),
    identify: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_out_capturing: vi.fn(),
    opt_in_capturing: vi.fn(),
  },
}));

it("emits the displayed quote ID so acceptance can be joined to exposure", async () => {
  configureAnalytics({
    learnerId: "11111111-1111-4111-8111-111111111111",
    optedOut: false,
    platform: "web",
  });
  vi.mocked(posthog.capture).mockClear();
  vi.spyOn(quoteAPI, "create").mockResolvedValue({
    id: "44444444-4444-4444-8444-444444444444",
    pricing_version: "v1",
    action: "lesson_content",
    currency: "USD",
    content_ceiling: 140,
    audio_ceiling: 0,
    daily_free_eligible: false,
    expires_at: new Date(Date.now() + 60000).toISOString(),
    accepted: false,
  });
  render(<QuotePanel />);
  fireEvent.click(screen.getByRole("button", { name: "Get action quote" }));
  await screen.findByText("Content ceiling: 140 credits");
  await waitFor(() =>
    expect(posthog.capture).toHaveBeenCalledWith(
      "action_quote_shown",
      expect.objectContaining({
        quote_id: "44444444-4444-4444-8444-444444444444",
        feature: "lesson_content",
        pricing_version: "v1",
        source: "client",
      }),
    ),
  );
});
