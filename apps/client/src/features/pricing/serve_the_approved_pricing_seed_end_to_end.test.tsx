import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, it, expect, vi, beforeEach } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import { PricingView } from "./PricingView";
import { api } from "../../lib/api";
import { configureAnalytics, exposePricing } from "../../lib/analytics";
import approved from "./fixtures/approved.json";
vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    init: vi.fn(),
    identify: vi.fn(),
    capture: vi.fn(),
    has_opted_out_capturing: () => false,
  },
}));
beforeEach(() => {
  vi.restoreAllMocks();
  sessionStorage.clear();
  configureAnalytics({
    learnerId: "8c940797-a555-421e-8dad-b4df96b02213",
    optedOut: false,
    platform: "web",
  });
});
function view() {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <PricingView />
    </QueryClientProvider>,
  );
}
describe("server pricing", () => {
  it("loads API values, grants no fictional balance and shows server costs", async () => {
    vi.spyOn(api, "pricing").mockResolvedValue({
      ...approved,
      sales_enabled: false,
      next_scheduled: null,
    });
    view();
    expect(screen.getByRole("status")).toHaveTextContent("Loading prices");
    expect(await screen.findByText("46 daily free credits")).toBeVisible();
    expect(screen.getByText("No wallet activity yet.")).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "Course creation costs" }),
    );
    expect(screen.getByText("190 credits")).toBeVisible();
  });
  it("offers retry after failure without a hardcoded fallback catalogue", async () => {
    vi.spyOn(api, "pricing").mockRejectedValue(new Error("offline"));
    view();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Prices are unavailable",
    );
    expect(screen.queryByText("46 daily free credits")).toBeNull();
    expect(screen.getByRole("button", { name: "Try again" })).toBeEnabled();
  });
  it("uses altered server prices and long benefits", async () => {
    const c = structuredClone(approved);
    c.actions.lesson_content.credits = 141;
    c.plans[0].benefits = ["Long benefit ".repeat(30)];
    vi.spyOn(api, "pricing").mockResolvedValue({
      ...c,
      sales_enabled: false,
      next_scheduled: null,
    });
    view();
    await screen.findByText("46 daily free credits");
    fireEvent.click(
      screen.getByRole("button", { name: "Course creation costs" }),
    );
    expect(screen.getByText("191 credits")).toBeVisible();
  });
  it("emits exposure through the rendered surface and never includes private content", async () => {
    configureAnalytics({
      learnerId: "9c940797-a555-421e-8dad-b4df96b02213",
      optedOut: false,
      platform: "web",
    });
    vi.mocked(posthog.capture).mockClear();
    vi.spyOn(api, "pricing").mockResolvedValue({
      ...approved,
      sales_enabled: false,
      next_scheduled: null,
    });
    view();
    await screen.findByText("46 daily free credits");
    fireEvent.click(screen.getByRole("button", { name: "Browse prices" }));
    fireEvent.click(screen.getByRole("button", { name: "Wallet" }));
    fireEvent.click(screen.getByRole("button", { name: "Browse prices" }));
    expect(posthog.capture).toHaveBeenCalledTimes(1);
    const properties = vi.mocked(posthog.capture).mock.calls[0][1];
    expect(Object.keys(properties ?? {}).sort()).toEqual([
      "app_version",
      "distinct_id",
      "platform",
      "pricing_version",
      "source",
      "surface",
    ]);
  });
  it("delivers one PostHog event per session/surface/version with UUID common properties and respects opt-out", () => {
    exposePricing(approved.version, "pack_list");
    exposePricing(approved.version, "pack_list");
    exposePricing(approved.version, "plan_list");
    expect(posthog.capture).toHaveBeenCalledTimes(2);
    expect(posthog.capture).toHaveBeenCalledWith("pricing_version_exposed", {
      distinct_id: "8c940797-a555-421e-8dad-b4df96b02213",
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      pricing_version: approved.version,
      surface: "pack_list",
    });
    configureAnalytics({
      learnerId: "8c940797-a555-421e-8dad-b4df96b02213",
      optedOut: true,
      platform: "web",
    });
    exposePricing("new", "quote");
    expect(posthog.capture).toHaveBeenCalledTimes(2);
    configureAnalytics({
      learnerId: "not-a-uuid",
      optedOut: false,
      platform: "web",
    });
    exposePricing("new", "quote");
    expect(posthog.capture).toHaveBeenCalledTimes(2);
  });
});
