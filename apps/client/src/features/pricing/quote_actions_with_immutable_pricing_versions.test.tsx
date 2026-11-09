import { render, screen, fireEvent } from "@testing-library/react";
import { it, expect, vi } from "vitest";
import { QuotePanel } from "./QuotePanel";
import { quoteAPI } from "./quotes";
it("discloses server ceilings, accepts terms and renews expired consent", async () => {
  vi.spyOn(quoteAPI, "create").mockResolvedValue({
    id: "quote",
    pricing_version: "original",
    action: "lesson_content",
    currency: "USD",
    content_ceiling: 140,
    audio_ceiling: 50,
    daily_free_eligible: false,
    expires_at: new Date(Date.now() + 600000).toISOString(),
    accepted: false,
  });
  vi.spyOn(quoteAPI, "accept").mockRejectedValue(new Error("expired"));
  render(<QuotePanel />);
  fireEvent.click(screen.getByRole("button", { name: "Get action quote" }));
  expect(await screen.findByText("Content ceiling: 140 credits")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Accept quote terms" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("renew");
  expect(screen.getByRole("button", { name: "Renew quote" })).toBeEnabled();
});

import posthog from "posthog-js/dist/module.no-external";
import { configureAnalytics } from "../../lib/analytics";
vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    init: vi.fn(),
    capture: vi.fn(),
    has_opted_out_capturing: () => false,
  },
}));
it("delivers quoted version through PostHog with UUID common fields and opt-out, without private data", async () => {
  const id = "8c940797-a555-421e-8dad-b4df96b02214";
  configureAnalytics({ learnerId: id, optedOut: false, platform: "web" });
  vi.mocked(posthog.capture).mockClear();
  vi.spyOn(quoteAPI, "create").mockResolvedValue({
    id: "original",
    pricing_version: "quoted-original",
    action: "lesson_content",
    currency: "USD",
    content_ceiling: 140,
    audio_ceiling: 50,
    daily_free_eligible: false,
    expires_at: new Date(Date.now() + 600000).toISOString(),
    accepted: false,
  });
  render(<QuotePanel />);
  fireEvent.click(screen.getByRole("button", { name: "Get action quote" }));
  await screen.findByText("Content ceiling: 140 credits");
  expect(posthog.capture).toHaveBeenCalledWith("pricing_version_exposed", {
    distinct_id: id,
    platform: "web",
    app_version: "0.1.0",
    source: "client",
    pricing_version: "quoted-original",
    surface: "quote",
  });
  fireEvent.click(screen.getByRole("button", { name: "Renew quote" }));
  await screen.findByText("Content ceiling: 140 credits");
  expect(posthog.capture).toHaveBeenCalledTimes(1);
  configureAnalytics({ learnerId: id, optedOut: true, platform: "web" });
  vi.spyOn(quoteAPI, "create").mockResolvedValue({
    id: "new",
    pricing_version: "opted-out",
    action: "lesson_content",
    currency: "USD",
    content_ceiling: 141,
    audio_ceiling: 0,
    daily_free_eligible: false,
    expires_at: new Date(Date.now() + 600000).toISOString(),
    accepted: false,
  });
  fireEvent.click(screen.getByRole("button", { name: "Renew quote" }));
  await screen.findByText("Content ceiling: 141 credits");
  expect(posthog.capture).toHaveBeenCalledTimes(1);
});
