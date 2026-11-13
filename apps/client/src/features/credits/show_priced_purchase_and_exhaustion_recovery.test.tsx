import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, expect, it, vi } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import approved from "../pricing/fixtures/approved.json";
import { PricingView } from "../pricing/PricingView";
import { api } from "../../lib/api";
import { checkoutAPI } from "./checkout";
import { configureAnalytics } from "../../lib/analytics";
import { quoteAPI } from "../pricing/quotes";
import { reservationAPI } from "./reservations";
import { credentialStore, session } from "../accounts/session";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: () => false,
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
    identify: vi.fn(),
  },
}));
const pending = {
  id: "test-order",
  state: "pending",
  illustrative: true,
  sku: "pack_try",
  pricing_version: approved.version,
  currency: "USD",
  amount_minor: 500,
  credits: 400,
  billing_period: "one_time",
  return_to: "/credits",
  quote_id: "",
} as const;
beforeEach(() => {
  vi.restoreAllMocks();
  sessionStorage.clear();
  configureAnalytics({
    learnerId: "8c940797-a555-421e-8dad-b4df96b02214",
    optedOut: false,
    platform: "web",
  });
  vi.spyOn(api, "pricing").mockResolvedValue({
    ...approved,
    sales_enabled: false,
    next_scheduled: null,
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
it("requires selection, binds configured terms, and never treats pending as success", async () => {
  const create = vi.spyOn(checkoutAPI, "create").mockResolvedValue(pending);
  const status = vi.spyOn(checkoutAPI, "get").mockResolvedValue(pending);
  view();
  fireEvent.click(await screen.findByRole("button", { name: "Browse prices" }));
  expect(
    await screen.findByRole("button", { name: "Preview checkout" }),
  ).toBeDisabled();
  fireEvent.click(screen.getByRole("radio", { name: /Try/ }));
  fireEvent.click(screen.getByRole("button", { name: "Preview checkout" }));
  expect(await screen.findByText(/Payment pending/)).toBeVisible();
  expect(create).toHaveBeenCalledWith(
    expect.objectContaining({
      sku: approved.packs[0].sku,
      pricing_version: approved.version,
      return_to: "/credits",
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Check payment status" }));
  await waitFor(() => expect(status).toHaveBeenCalledWith("test-order"));
  expect(screen.queryByText("Credits added")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Cancel preview" }));
  expect(
    screen.getByText("No credits were added. Your draft is ready."),
  ).toBeVisible();
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_checkout_requested",
    expect.objectContaining({
      distinct_id: "8c940797-a555-421e-8dad-b4df96b02214",
      offer_id: approved.packs[0].sku,
    }),
  );
  expect(JSON.stringify(vi.mocked(posthog.capture).mock.calls)).not.toContain(
    "return_to",
  );
});
it("discloses full annual amount and monthly issuance with no selected plan", async () => {
  view();
  fireEvent.click(await screen.findByRole("button", { name: "Browse prices" }));
  fireEvent.click(screen.getByRole("button", { name: "Plans" }));
  fireEvent.click(screen.getByRole("radio", { name: "Yearly" }));
  expect(
    screen.getByRole("radio", { name: /Plus.*yearly/i }),
  ).not.toBeChecked();
  expect(screen.getByText("$192.00")).toBeVisible();
  expect(screen.getByText(/Monthly equivalent:.*16.00/)).toBeVisible();
  expect(screen.getByText(/1,600 credits each service month/)).toBeVisible();
  expect(screen.getByText(/Taxes/)).toBeVisible();
});
it("keeps cached offers visible but disables checkout offline and respects opt out", async () => {
  configureAnalytics({
    learnerId: "8c940797-a555-421e-8dad-b4df96b02214",
    optedOut: true,
    platform: "web",
  });
  view();
  fireEvent.click(await screen.findByRole("button", { name: "Browse prices" }));
  fireEvent.click(screen.getByRole("radio", { name: /Try/ }));
  vi.spyOn(api, "pricing").mockRejectedValue(new Error("offline"));
  fireEvent.click(screen.getByRole("button", { name: "Refresh prices" }));
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(
    screen.getByRole("button", { name: "Preview checkout" }),
  ).toBeDisabled();
  expect(posthog.capture).not.toHaveBeenCalled();
});

it("retains the original action through checkout and Resume never reserves or sends", async () => {
  const quote = {
    id: "saved-quote",
    pricing_version: approved.version,
    action: "quick_note",
    currency: "USD",
    content_ceiling: approved.actions.quick_note.credits,
    audio_ceiling: 0,
    daily_free_eligible: true,
    expires_at: new Date(Date.now() + 600_000).toISOString(),
    accepted: false,
  };
  const create = vi.spyOn(quoteAPI, "create").mockResolvedValue(quote);
  vi.spyOn(quoteAPI, "accept").mockResolvedValue({ accepted: true });
  vi.spyOn(quoteAPI, "get").mockResolvedValue({ ...quote, accepted: true });
  const reserve = vi
    .spyOn(reservationAPI, "reserve")
    .mockRejectedValue(
      new Error("Not enough eligible credits. Next daily grant: tomorrow"),
    );
  vi.spyOn(checkoutAPI, "create").mockResolvedValue({
    ...pending,
    quote_id: quote.id,
  });
  vi.spyOn(checkoutAPI, "get").mockResolvedValue({
    ...pending,
    quote_id: quote.id,
  });
  view();
  fireEvent.click(
    await screen.findByRole("button", { name: "Course creation costs" }),
  );
  fireEvent.change(screen.getByRole("combobox", { name: "Action" }), {
    target: { value: "quick_note" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Get action quote" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Accept quote terms" }),
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Reserve credits" }),
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "View credit options" }),
  );
  fireEvent.click(screen.getByRole("radio", { name: /Try/ }));
  fireEvent.click(screen.getByRole("button", { name: "Preview checkout" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Return to saved work" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Resume action" }));
  expect(screen.getByRole("combobox", { name: "Action" })).toHaveValue(
    "quick_note",
  );
  expect(await screen.findByText(/Terms accepted/)).toBeVisible();
  expect(create).toHaveBeenCalledTimes(1);
  expect(reserve).toHaveBeenCalledTimes(1);
  for (const event of [
    "paywall_viewed",
    "credits_upsell_shown",
    "pricing_version_exposed",
    "ui_purchase_option_selected",
    "ui_checkout_requested",
  ])
    expect(posthog.capture).toHaveBeenCalledWith(
      event,
      expect.objectContaining({
        distinct_id: "8c940797-a555-421e-8dad-b4df96b02214",
        source: "client",
        platform: "web",
        app_version: "0.1.0",
      }),
    );
});

it("disables external purchase controls for native platforms", async () => {
  vi.spyOn(navigator, "userAgent", "get").mockReturnValue("Duolify Android");
  view();
  fireEvent.click(await screen.findByRole("button", { name: "Browse prices" }));
  fireEvent.click(screen.getByRole("radio", { name: /Try/ }));
  expect(
    screen.getByRole("button", { name: "Preview checkout" }),
  ).toBeDisabled();
  expect(screen.getByText(/Native store purchases unavailable/)).toBeVisible();
  expect(
    screen.queryByRole("link", { name: /checkout/i }),
  ).not.toBeInTheDocument();
});

it("does not reuse selection after a pricing version changes", async () => {
  view();
  fireEvent.click(await screen.findByRole("button", { name: "Browse prices" }));
  fireEvent.click(screen.getByRole("radio", { name: /Try/ }));
  expect(
    screen.getByRole("button", { name: "Preview checkout" }),
  ).toBeEnabled();
  vi.spyOn(api, "pricing").mockResolvedValue({
    ...approved,
    version: "changed-prices",
    sales_enabled: false,
    next_scheduled: null,
  });
  fireEvent.click(screen.getByRole("button", { name: "Refresh prices" }));
  await screen.findByText(/Approved pricing · changed-prices/);
  expect(screen.getByRole("radio", { name: /Try/ })).not.toBeChecked();
  expect(
    screen.getByRole("button", { name: "Preview checkout" }),
  ).toBeDisabled();
});

it("keeps a failed daily reset unverified instead of claiming reconciliation", async () => {
  vi.spyOn(credentialStore, "read").mockResolvedValue({
    access_token: "fixture",
    refresh_token: "fixture",
    expires_in: 900,
  });
  vi.spyOn(session, "request")
    .mockResolvedValueOnce({
      available: 0,
      daily_free: {
        available: 0,
        granted: approved.daily_free.credits,
        resets_at: "2026-11-10T00:00:00Z",
        timezone: "UTC",
      },
      purchased: 0,
      subscription: 0,
      reserved: 0,
      recent_activity: [],
    })
    .mockRejectedValue(new Error("offline"));
  view();
  await screen.findByText(/Daily free credits reset at 00:00 UTC/);
  fireEvent.click(screen.getByRole("button", { name: "Wait for daily reset" }));
  expect(
    await screen.findByText(
      "Reset awaiting sync. Connect and check again; your work is saved.",
    ),
  ).toBeVisible();
  expect(
    screen.queryByText(/Daily reset checked with the server/),
  ).not.toBeInTheDocument();
  expect(screen.getByText("Balance not verified")).toBeVisible();
});
