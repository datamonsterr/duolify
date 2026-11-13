import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { api } from "../../lib/api";
import approved from "./fixtures/approved.json";
import { PricingView } from "./PricingView";
import { quoteAPI } from "./quotes";

it("shows the scheduled catalogue after a flip while accepted quote terms stay immutable", async () => {
  const before = {
    ...approved,
    version: "schedule-before",
    actions: { ...approved.actions, preview: { credits: 25 } },
    sales_enabled: false,
    next_scheduled: null,
  };
  const after = {
    ...approved,
    version: "schedule-after",
    actions: { ...approved.actions, preview: { credits: 20 } },
    sales_enabled: false,
    next_scheduled: null,
  };
  const rollback = {
    ...approved,
    version: "rollback-higher",
    actions: { ...approved.actions, preview: { credits: 25 } },
    sales_enabled: false,
    next_scheduled: null,
  };
  vi.spyOn(api, "pricing")
    .mockResolvedValueOnce(before)
    .mockResolvedValueOnce(after)
    .mockResolvedValueOnce(rollback);
  vi.spyOn(quoteAPI, "create").mockResolvedValue({
    id: "quote-before-flip",
    pricing_version: "schedule-before",
    action: "lesson_content",
    currency: "USD",
    content_ceiling: 140,
    audio_ceiling: 50,
    daily_free_eligible: false,
    expires_at: new Date(Date.now() + 600_000).toISOString(),
    accepted: false,
  });
  vi.spyOn(quoteAPI, "accept").mockResolvedValue({ accepted: true });
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <PricingView />
    </QueryClientProvider>,
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Course creation costs" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Get action quote" }));
  expect(
    await screen.findByText(/Original terms · schedule-before/),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Accept quote terms" }));
  expect(
    await screen.findByText(/Terms accepted · original version retained/),
  ).toBeVisible();

  fireEvent.click(screen.getByRole("button", { name: "Refresh prices" }));
  expect(await screen.findByText("20 credits", { exact: true })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Refresh prices" }));
  expect(await screen.findByText("25 credits", { exact: true })).toBeVisible();
  expect(screen.getByText(/Original terms · schedule-before/)).toBeVisible();
});
