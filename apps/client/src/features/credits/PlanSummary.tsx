import type { WalletResponse } from "./types";
import { useUiPreferences } from "../design/localization/Preferences";

export function PlanSummary({ wallet }: { wallet: WalletResponse }) {
  const { number, date } = useUiPreferences();
  const plan = wallet.plan;
  if (!plan) return null;
  return (
    <section className="wallet-card wallet-plan" aria-label="Your subscription">
      <h2>Your plan · {plan.name}</h2>
      <p>
        {wallet.unverified
          ? "Plan status awaiting connection"
          : plan.active
            ? plan.billing_period === "yearly"
              ? "Active · Annual"
              : "Active · Monthly"
            : plan.revoked
              ? "Refunded · issued credits remain available"
              : "Expired · issued credits remain available"}
      </p>
      <p>
        Paid through{" "}
        {date(new Date(plan.paid_through), wallet.daily_free.timezone)} · Stripe
      </p>
      <p>
        {number(plan.monthly_credits)} credits each service month. Issued
        credits never expire.
      </p>
      {plan.cancel_at_period_end && !plan.revoked && (
        <p>
          Auto-renew off · funded allowances continue through{" "}
          {date(new Date(plan.paid_through), wallet.daily_free.timezone)}.
        </p>
      )}
      {plan.next_allowance_at && !plan.revoked && (
        <p>
          Next allowance:{" "}
          {date(new Date(plan.next_allowance_at), wallet.daily_free.timezone)} ·{" "}
          {number(plan.monthly_credits)} credits.
        </p>
      )}
      <p>Pricing / entitlement version: {plan.pricing_version}</p>
      {plan.active && !wallet.unverified && (
        <ul>
          {plan.benefits.map((benefit) => (
            <li key={benefit}>{benefit}</li>
          ))}
        </ul>
      )}
      <a className="wallet-btn-options" href={plan.management_url}>
        Manage billing with Stripe
      </a>
    </section>
  );
}
