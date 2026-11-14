import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { PlanSummary } from "./PlanSummary";
import type { WalletResponse } from "./types";
import approved from "../pricing/fixtures/approved.json";
it.each(["active", "cancelled", "refunded"])(
  "shows annual funded schedule: %s",
  (state) => {
    const plan = approved.plans[0];
    const wallet = {
      daily_free: { timezone: "UTC" },
      plan: {
        plan_id: plan.id,
        name: plan.name,
        pricing_version: approved.version,
        monthly_credits: plan.monthly_credits,
        benefits: plan.benefits,
        paid_through: "2027-01-31T12:00:00Z",
        active: state !== "refunded",
        billing_period: "yearly",
        cancel_at_period_end: state === "cancelled",
        revoked: state === "refunded",
        next_allowance_at:
          state === "refunded" ? undefined : "2026-02-28T12:00:00Z",
        management_url: "/credits/billing",
      },
    } as WalletResponse;
    render(<PlanSummary wallet={wallet} />);
    expect(
      screen.getByText(
        state === "refunded"
          ? "Refunded · affected credits reversed"
          : "Active · Annual",
      ),
    ).toBeVisible();
    if (state === "cancelled")
      expect(screen.getByText(/Auto-renew off/)).toBeVisible();
    if (state === "refunded")
      expect(screen.queryByText(/Next allowance/)).toBeNull();
    else expect(screen.getByText(/Next allowance/)).toBeVisible();
    expect(screen.getByText(/Issued credits never expire/)).toBeVisible();
  },
);
