import { useRef, useState } from "react";
import { api } from "../../lib/api";
import { session } from "../accounts/session";
import type { Pricing } from "../pricing/types";
import { Button } from "../design/Button";
import type { WalletResponse } from "./types";
import { useUiPreferences } from "../design/localization/Preferences";

export function PlanSummary({ wallet }: { wallet: WalletResponse }) {
  const { number, date, currency } = useUiPreferences();
  const [updated, setUpdated] = useState<{
    source: WalletResponse;
    value: WalletResponse;
  }>();
  const [catalogue, setCatalogue] = useState<Pricing>();
  const [selected, setSelected] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  const intent = useRef<{ kind: string; sku: string; id: string } | undefined>(
    undefined,
  );
  const plan = (updated?.source === wallet ? updated.value : wallet).plan;
  async function change(kind: "plan" | "cancel" | "resume") {
    setBusy(true);
    setError(false);
    const sku = kind === "plan" ? selected : "";
    if (intent.current?.kind !== kind || intent.current?.sku !== sku)
      intent.current = { kind, sku, id: crypto.randomUUID() };
    try {
      await session.request("/v1/credits/subscription/change", "POST", {
        request_id: intent.current!.id,
        kind,
        sku,
        pricing_version: catalogue?.version ?? "",
      });
      setUpdated({
        source: wallet,
        value: await session.request<WalletResponse>("/v1/me/credits"),
      });
      setCatalogue(undefined);
      intent.current = undefined;
    } catch {
      setError(true);
    } finally {
      setBusy(false);
    }
  }
  async function showPlans() {
    setBusy(true);
    setError(false);
    try {
      setCatalogue(await api.pricing());
    } catch {
      setError(true);
    } finally {
      setBusy(false);
    }
  }
  if (!plan) return null;
  return (
    <section className="wallet-card wallet-plan" aria-label="Your subscription">
      <h2>Your plan · {plan.name}</h2>
      <p>
        {wallet.unverified
          ? "Plan status awaiting connection"
          : plan.status === "past-due"
            ? plan.active
              ? "Payment failed · paid benefits continue through the paid-through date"
              : "Payment failed · paid benefits ended; issued credits remain available"
            : plan.status === "disputed"
              ? "Disputed · issued credits remain available"
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
      {plan.active && !plan.cancel_at_period_end && (
        <p>
          Next renewal:{" "}
          {date(new Date(plan.paid_through), wallet.daily_free.timezone)}.
        </p>
      )}
      {plan.pending_plan && (
        <div role="status">
          <p>
            Switching to {plan.pending_plan.name} starts{" "}
            {date(
              new Date(plan.pending_plan.effective_at),
              wallet.daily_free.timezone,
            )}
            .
          </p>
          <p>
            No proration. Your current paid benefits continue until then; the
            new plan requires a paid renewal.
          </p>
        </div>
      )}
      {plan.active && !wallet.unverified && (
        <div>
          {!plan.pending_plan && !plan.cancel_at_period_end && (
            <Button
              variant="secondary"
              disabled={busy}
              onClick={() => void showPlans()}
            >
              Change plan
            </Button>
          )}
          <Button
            variant="quiet"
            disabled={busy}
            onClick={() =>
              void change(plan.cancel_at_period_end ? "resume" : "cancel")
            }
          >
            {plan.cancel_at_period_end
              ? "Turn on auto-renew"
              : "Turn off auto-renew"}
          </Button>
          {catalogue && (
            <div>
              <label>
                New plan
                <select
                  aria-label="New plan"
                  value={selected}
                  onChange={(e) => setSelected(e.target.value)}
                >
                  <option value="">Choose a plan</option>
                  {catalogue.plans
                    .filter((p) => p.id !== plan.plan_id)
                    .map((p) => {
                      const offer =
                        plan.billing_period === "yearly" ? p.yearly : p.monthly;
                      return (
                        <option key={p.id} value={offer.sku}>
                          {p.name} ·{" "}
                          {currency(offer.amount_minor / 100, offer.currency)} /{" "}
                          {plan.billing_period ?? "monthly"}
                        </option>
                      );
                    })}
                </select>
              </label>
              <p>Starts at next renewal. No proration or immediate charge.</p>
              <Button
                disabled={busy || !selected}
                onClick={() => void change("plan")}
              >
                Confirm plan change
              </Button>
            </div>
          )}
        </div>
      )}
      {busy && <p role="status">Updating billing…</p>}
      {error && (
        <p role="alert">
          Could not update billing. Check your connection and retry your chosen
          action.
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
