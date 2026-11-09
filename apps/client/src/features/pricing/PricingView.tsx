import { useUiPreferences } from "../design/localization/Preferences";
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { exposePricing } from "../../lib/analytics";
import { QuotePanel } from "./QuotePanel";
import { pricingQueryOptions } from "./query";
import { Button } from "../design/Button";
import "./pricing.css";

const messages = {
  title: "Credits",
  loading: "Loading prices…",
  unavailable: "Prices are unavailable. Check your connection and try again.",
  retry: "Try again",
  wallet: "Wallet",
  packs: "Credit packs",
  plans: "Plans",
  creation: "Course creation costs",
  browse: "Browse prices",
  empty: "No wallet activity yet.",
  readonly:
    "Price catalogue only. Purchases will be available after billing verification.",
  checkout: "Checkout unavailable",
  paid: "Paid credits roll over and never expire, including after cancellation. Daily free credits are spent first for notes and tutor chat and expire at the end of your local day.",
  annual:
    "Billed once a year; credits granted each paid service month. Renews automatically; cancel any time. Taxes are additional.",
} as const;
type Surface = "wallet" | "pack_list" | "plan_list" | "quote";
export function PricingView() {
  const { number: formatCredits, currency, date } = useUiPreferences();
  const formatMoney = (minor: number, code: string) =>
    currency(minor / 100, code);
  const [surface, setSurface] = useState<Surface>("wallet");
  const [interval, setInterval] = useState<"monthly" | "yearly">("monthly");
  const [selection, setSelection] = useState("");
  const heading = useRef<HTMLHeadingElement>(null);
  const query = useQuery(pricingQueryOptions());
  const c = query.data;
  const stale = query.isError && !!c;
  useEffect(() => {
    if (c && surface !== "wallet" && surface !== "quote")
      exposePricing(c.version, surface);
  }, [c, surface]);
  function navigate(next: Surface) {
    setSurface(next);
    setSelection("");
    heading.current?.focus();
  }
  return (
    <section className="pricing" aria-labelledby="pricing-heading">
      <h1 id="pricing-heading" ref={heading} tabIndex={-1}>
        {messages.title}
      </h1>
      <Button
        variant="quiet"
        onClick={() => void query.refetch()}
        disabled={query.isFetching}
      >
        Refresh prices
      </Button>
      <nav aria-label="Credit views" className="pricing-nav">
        <Button
          variant="quiet"
          aria-current={surface === "wallet" ? "page" : undefined}
          onClick={() => navigate("wallet")}
        >
          {messages.wallet}
        </Button>
        <Button
          variant="quiet"
          aria-current={
            surface === "pack_list" || surface === "plan_list"
              ? "page"
              : undefined
          }
          onClick={() => navigate("pack_list")}
        >
          {messages.browse}
        </Button>
        <Button
          variant="quiet"
          aria-current={surface === "quote" ? "page" : undefined}
          onClick={() => navigate("quote")}
        >
          {messages.creation}
        </Button>
      </nav>
      {query.isPending && <p role="status">{messages.loading}</p>}
      {query.isError && (
        <div role="alert" className="pricing-warning">
          <p>{messages.unavailable}</p>
          <Button variant="secondary" onClick={() => void query.refetch()}>
            {messages.retry}
          </Button>
        </div>
      )}
      {c && (
        <>
          <p className="pricing-version">
            {stale ? "Offline · Last known prices" : "Approved pricing"} ·{" "}
            {c.version} · Effective {date(new Date(c.effective_from), "UTC")}{" "}
            (UTC)
          </p>
          {c.next_scheduled && (
            <p>
              Next catalogue: {c.next_scheduled.version} ·{" "}
              {date(
                new Date(c.next_scheduled.effective_from),
                Intl.DateTimeFormat().resolvedOptions().timeZone,
                { dateStyle: "medium", timeStyle: "short" },
              )}
            </p>
          )}
          {surface === "wallet" && (
            <>
              <div className="pricing-card pricing-credit">
                <h2>
                  {formatCredits(c.daily_free.credits)} daily free credits
                </h2>
                <p>
                  For quick notes and tutor chat. Resets at the end of your
                  local day.
                </p>
              </div>
              <div className="pricing-card">
                <h2>Wallet activity</h2>
                <p>{messages.empty}</p>
                <p>
                  Balances and reservations appear here when your wallet is
                  connected.
                </p>
              </div>
              <p>{messages.paid}</p>
              <Button onClick={() => navigate("pack_list")}>
                See packs and plans
              </Button>
            </>
          )}
          {(surface === "pack_list" || surface === "plan_list") && (
            <>
              <h2>Make room for your next idea.</h2>
              <div className="pricing-nav" aria-label="Offer type">
                <Button
                  variant="quiet"
                  aria-pressed={surface === "pack_list"}
                  onClick={() => navigate("pack_list")}
                >
                  {messages.packs}
                </Button>
                <Button
                  variant="quiet"
                  aria-pressed={surface === "plan_list"}
                  onClick={() => navigate("plan_list")}
                >
                  {messages.plans}
                </Button>
              </div>
              {surface === "plan_list" && (
                <fieldset className="pricing-period">
                  <legend>Billing period</legend>
                  {(["monthly", "yearly"] as const).map((period) => (
                    <label key={period}>
                      <input
                        type="radio"
                        name="period"
                        checked={interval === period}
                        onChange={() => {
                          setInterval(period);
                          setSelection("");
                        }}
                      />
                      {period === "monthly" ? "Monthly" : "Yearly"}
                    </label>
                  ))}
                </fieldset>
              )}
              <fieldset className="pricing-offers">
                <legend>
                  {surface === "pack_list"
                    ? "Choose a credit pack"
                    : "Choose a plan"}
                </legend>
                {surface === "pack_list"
                  ? c.packs.map((p) => (
                      <label className="pricing-card" key={p.sku}>
                        <input
                          type="radio"
                          name="offer"
                          aria-label={p.name}
                          checked={selection === p.sku}
                          onChange={() => setSelection(p.sku)}
                        />
                        <h3>{formatCredits(p.credits)} credits</h3>
                        <strong>
                          {formatMoney(p.amount_minor, p.currency)}
                        </strong>
                        <p>{p.name} · Paid credits never expire</p>
                      </label>
                    ))
                  : c.plans.map((p) => (
                      <label className="pricing-card" key={p.id}>
                        <input
                          type="radio"
                          name="offer"
                          aria-label={p.name}
                          checked={selection === p[interval].sku}
                          onChange={() => setSelection(p[interval].sku)}
                        />
                        <h3>
                          <bdi>{p.name}</bdi>
                        </h3>
                        <strong>
                          {formatMoney(
                            p[interval].amount_minor,
                            p[interval].currency,
                          )}
                        </strong>
                        <span>
                          {" "}
                          / {interval === "monthly" ? "month" : "year"}
                        </span>
                        <p>
                          {formatCredits(p.monthly_credits)} credits each
                          service month
                        </p>
                        <ul>
                          {p.benefits.map((b) => (
                            <li key={b}>{b}</li>
                          ))}
                        </ul>
                        <p>
                          {formatCredits(p.concurrent_generation)} concurrent
                          generation{" "}
                          {p.concurrent_generation === 1 ? "job" : "jobs"}
                        </p>
                      </label>
                    ))}
              </fieldset>
              <p>{messages.paid}</p>
              {surface === "plan_list" && <p>{messages.annual}</p>}
              <p role="status">{messages.readonly}</p>
              <Button
                variant="quiet"
                disabled
                aria-describedby="sale-requirements"
              >
                {messages.checkout}
              </Button>
              <p id="sale-requirements">
                Live sales require verified payment products and launch
                approval.
              </p>
              <Button variant="quiet" onClick={() => navigate("wallet")}>
                Wait for daily reset
              </Button>
            </>
          )}
          {surface === "quote" && (
            <>
              <h2>Create a course · price guide</h2>
              <QuotePanel narrationCharacters={c.narration.characters} />
              <p>
                Review costs before confirming any generation. These are
                catalogue prices; an API quote confirms the final charge.
              </p>
              <div className="pricing-costs">
                <div className="pricing-card">
                  <h3>Course preview</h3>
                  <strong>
                    {formatCredits(c.actions.preview.credits)} credits
                  </strong>
                  <p>Sample outline and example slides.</p>
                </div>
                <div className="pricing-card">
                  <h3>Course outline</h3>
                  <strong>
                    {formatCredits(c.actions.outline.credits)} credits
                  </strong>
                  <p>Future lessons are prepared only when reached.</p>
                </div>
                <aside className="pricing-card">
                  <h3>Lesson with narration</h3>
                  <strong>
                    {formatCredits(
                      c.actions.lesson_content.credits + c.narration.credits,
                    )}{" "}
                    credits
                  </strong>
                  <p>
                    {formatCredits(c.actions.lesson_content.credits)} content +{" "}
                    {formatCredits(c.narration.credits)} narration for{" "}
                    {formatCredits(c.narration.characters)} newly synthesized
                    characters.
                  </p>
                  <p>
                    Narration rounds up once across all new characters. Cached
                    replay: {formatCredits(c.actions.cached_replay.credits)}{" "}
                    credits.
                  </p>
                </aside>
              </div>
              <dl>
                {["tutor_short", "tutor_long", "quick_note"].map((k) => (
                  <div key={k}>
                    <dt>
                      {
                        (
                          {
                            tutor_short: "Tutor short",
                            tutor_long: "Tutor long",
                            quick_note: "Quick note",
                          } as Record<string, string>
                        )[k]
                      }
                    </dt>
                    <dd>{formatCredits(c.actions[k].credits)} credits</dd>
                  </div>
                ))}
              </dl>
              <p>{messages.readonly}</p>
              <Button onClick={() => navigate("pack_list")}>
                Review packs and plans
              </Button>
            </>
          )}
        </>
      )}
    </section>
  );
}
