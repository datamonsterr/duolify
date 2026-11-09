import { useUiPreferences } from "../design/localization/Preferences";
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { exposePricing } from "../../lib/analytics";
import type { Pricing } from "./types";
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
function valid(c: Pricing): boolean {
  return (
    !!c &&
    typeof c.version === "string" &&
    Number.isFinite(Date.parse(c.effective_from)) &&
    c.currency === "USD" &&
    [
      "preview",
      "outline",
      "lesson_content",
      "narration",
      "tutor_short",
      "tutor_long",
      "quick_note",
      "cached_replay",
    ].every(
      (k) =>
        Number.isSafeInteger(c.actions?.[k]?.credits) &&
        c.actions[k].credits >= 0,
    ) &&
    Number.isSafeInteger(c.daily_free?.credits) &&
    c.daily_free.credits >= 0 &&
    c.narration?.characters > 0 &&
    Array.isArray(c.packs) &&
    c.packs.length > 0 &&
    Array.isArray(c.plans) &&
    c.plans.length > 0
  );
}
function cached(): Pricing | undefined {
  try {
    const value: Pricing = JSON.parse(
      sessionStorage.getItem("pricing:last-known") || "null",
    );
    return valid(value) ? value : undefined;
  } catch {
    return undefined;
  }
}

export function PricingView() {
  const { number, currency, date } = useUiPreferences();
  const money = (minor: number, code: string) => currency(minor / 100, code);
  const [surface, setSurface] = useState<Surface>("wallet");
  const [interval, setInterval] = useState<"monthly" | "yearly">("monthly");
  const [selection, setSelection] = useState("");
  const [lastKnown] = useState(cached);
  const heading = useRef<HTMLHeadingElement>(null);
  const query = useQuery({
    queryKey: ["pricing"],
    queryFn: async () => {
      const c = await api.pricing();
      if (!valid(c)) throw new Error("Invalid pricing response");
      return c;
    },
    retry: false,
    staleTime: 60_000,
    networkMode: "always",
  });
  const c = query.data ?? (query.isError ? lastKnown : undefined);
  const stale = query.isError && !!c;
  useEffect(() => {
    if (query.data) {
      try {
        sessionStorage.setItem(
          "pricing:last-known",
          JSON.stringify(query.data),
        );
      } catch {
        /* Read-only display works without storage. */
      }
    }
  }, [query.data]);
  useEffect(() => {
    if (c && surface !== "wallet") exposePricing(c.version, surface);
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
                <h2>{number(c.daily_free.credits)} daily free credits</h2>
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
                        <h3>{number(p.credits)} credits</h3>
                        <strong>{money(p.amount_minor, p.currency)}</strong>
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
                          {money(
                            p[interval].amount_minor,
                            p[interval].currency,
                          )}
                        </strong>
                        <span>
                          {" "}
                          / {interval === "monthly" ? "month" : "year"}
                        </span>
                        <p>
                          {number(p.monthly_credits)} credits each service month
                        </p>
                        <ul>
                          {p.benefits.map((b) => (
                            <li key={b}>{b}</li>
                          ))}
                        </ul>
                        <p>
                          {number(p.concurrent_generation)} concurrent
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
              <p>
                Review costs before confirming any generation. These are
                catalogue prices; an API quote confirms the final charge.
              </p>
              <div className="pricing-costs">
                <div className="pricing-card">
                  <h3>Course preview</h3>
                  <strong>{number(c.actions.preview.credits)} credits</strong>
                  <p>Sample outline and example slides.</p>
                </div>
                <div className="pricing-card">
                  <h3>Course outline</h3>
                  <strong>{number(c.actions.outline.credits)} credits</strong>
                  <p>Future lessons are prepared only when reached.</p>
                </div>
                <aside className="pricing-card">
                  <h3>Lesson with narration</h3>
                  <strong>
                    {number(
                      c.actions.lesson_content.credits + c.narration.credits,
                    )}{" "}
                    credits
                  </strong>
                  <p>
                    {number(c.actions.lesson_content.credits)} content +{" "}
                    {number(c.narration.credits)} narration for{" "}
                    {number(c.narration.characters)} newly synthesized
                    characters.
                  </p>
                  <p>
                    Narration rounds up once across all new characters. Cached
                    replay: {number(c.actions.cached_replay.credits)} credits.
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
                    <dd>{number(c.actions[k].credits)} credits</dd>
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
