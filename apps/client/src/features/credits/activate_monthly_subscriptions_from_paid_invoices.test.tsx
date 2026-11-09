import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { WalletView } from "./WalletView";
import type { WalletResponse } from "./types";
import approved from "../pricing/fixtures/approved.json";

it.each([true, false])(
  "shows funded plan independently of the nonexpiring balance: active=%s",
  (active) => {
    const plan = approved.plans[0];
    const wallet = {
      available: plan.monthly_credits,
      purchased: 0,
      subscription: plan.monthly_credits,
      reserved: 0,
      daily_free: { available: 0, granted: 0, resets_at: "", timezone: "UTC" },
      recent_activity: [],
      plan: {
        plan_id: plan.id,
        name: plan.name,
        pricing_version: approved.version,
        entitlement_version: approved.version,
        monthly_credits: plan.monthly_credits,
        benefits: plan.benefits,
        concurrent_generation: plan.concurrent_generation,
        paid_through: "2026-12-01T00:00:00Z",
        channel: "stripe",
        management_url: "/credits/billing",
        active,
      },
    } as WalletResponse;
    render(<WalletView wallet={wallet} trackView={false} />);
    expect(
      screen.getByRole("heading", { name: `Your plan · ${plan.name}` }),
    ).toBeVisible();
    expect(screen.getByText(/Paid through/)).toHaveTextContent("2026");
    expect(
      screen.getByRole("link", { name: "Manage billing with Stripe" }),
    ).toHaveAttribute("href", "/credits/billing");
    expect(
      screen.getByText(
        active
          ? "Active · Monthly"
          : "Expired · issued credits remain available",
      ),
    ).toBeVisible();
    expect(screen.getByText(new RegExp(approved.version))).toBeVisible();
  },
);
