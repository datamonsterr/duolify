import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { PlanSummary } from "./PlanSummary";
import type { WalletResponse } from "./types";
import approved from "../pricing/fixtures/approved.json";
it.each([true, false])(
  "shows failed payment without extending benefits: active=%s",
  (active) => {
    const plan = approved.plans[1];
    const wallet = {
      daily_free: { timezone: "UTC" },
      plan: {
        plan_id: plan.id,
        name: plan.name,
        monthly_credits: plan.monthly_credits,
        pricing_version: approved.version,
        entitlement_version: approved.version,
        benefits: plan.benefits,
        concurrent_generation: plan.concurrent_generation,
        paid_through: "2026-12-01T00:00:00Z",
        active,
        status: "past-due",
        channel: "stripe",
        management_url: "/credits/billing",
        pending_plan: {
          name: approved.plans[0].name,
          effective_at: "2026-12-01T00:00:00Z",
        },
      },
    } as WalletResponse;
    render(<PlanSummary wallet={wallet} />);
    expect(screen.getByText(/Payment failed ·/)).toHaveTextContent(
      active ? "paid benefits continue" : "paid benefits ended",
    );
    expect(screen.getByText(/Switching to/)).toHaveTextContent(
      approved.plans[0].name,
    );
    expect(screen.getByText(/No proration/)).toBeVisible();
    expect(screen.queryByText(plan.benefits[0])).toBe(
      active ? screen.getByText(plan.benefits[0]) : null,
    );
  },
);
