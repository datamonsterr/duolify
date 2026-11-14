import { useUiPreferences } from "../design/localization/Preferences";
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { exposePricing, trackPurchase } from "../../lib/analytics";
import { CheckoutReturn } from "../credits/CheckoutReturn";
import { checkoutAPI, recoveryID } from "../credits/checkout";
import type { ActionQuote } from "./quotes";
import { QuotePanel } from "./QuotePanel";
import { pricingQueryOptions } from "./query";
import { Button } from "../design/Button";
import { WalletView } from "../credits/WalletView";
import { PlanSummary } from "../credits/PlanSummary";
import type { WalletResponse } from "../credits/types";
import { credentialStore, session } from "../accounts/session";
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
  const [selectedSKU, setSelection] = useState("");
  const [selectionVersion, setSelectionVersion] = useState("");
  const [orderId, setOrderId] = useState(() => recoveryID("order"));
  const [recoveryQuoteId, setRecoveryQuoteId] = useState(() =>
    recoveryID("quote"),
  );
  const [recoveryQuote, setRecoveryQuote] = useState<ActionQuote>();
  const [review, setReview] = useState(false);
  const [quoteVisited, setQuoteVisited] = useState(false);
  const [checkoutBusy, setCheckoutBusy] = useState(false);
  const [checkoutError, setCheckoutError] = useState(false);
  const [waiting, setWaiting] = useState(false);
  const platform = /Android/i.test(navigator.userAgent)
    ? "android"
    : /iPhone|iPad/i.test(navigator.userAgent)
      ? "ios"
      : "__TAURI_INTERNALS__" in window
        ? "desktop"
        : "web";
  const native = platform === "ios" || platform === "android";
  const heading = useRef<HTMLHeadingElement>(null);
  const query = useQuery(pricingQueryOptions());
  const c = query.data;
  const selection = selectionVersion === c?.version ? selectedSKU : "";
  const stale = query.isError && !!c;

  const walletQuery = useQuery({
    queryKey: ["wallet", session.current()?.id],
    queryFn: async () => {
      const tokens = await credentialStore.read();
      if (!tokens) return null;
      return await session.request<WalletResponse>("/v1/me/credits");
    },
    retry: false,
  });

  const fallbackCredits = 0; // @noncommercial-number
  const defaultWallet: WalletResponse = {
    available: fallbackCredits,
    daily_free: {
      available: fallbackCredits,
      granted: c ? c.daily_free.credits : fallbackCredits, // @noncommercial-number
      resets_at: "",
      timezone: "UTC",
    },
    purchased: 0,
    subscription: 0,
    reserved: 0,
    reserved_note: "0 reserved",
    recent_activity: [],
    unverified: true,
  };

  const activeWallet = {
    ...(walletQuery.data ?? defaultWallet),
    unverified:
      walletQuery.isError || !walletQuery.data || walletQuery.data.unverified,
  };
  useEffect(() => {
    if (c && surface !== "wallet" && surface !== "quote")
      exposePricing(c.version, surface);
  }, [c, surface]);
  function navigate(next: Surface) {
    setSurface(next);
    setSelection("");
    setCheckoutError(false);
    if (next === "quote") setQuoteVisited(true);
    heading.current?.focus();
  }
  function showRecovery(quote: ActionQuote) {
    setRecoveryQuote(quote);
    setRecoveryQuoteId(quote.id);
    recoveryID("quote", quote.id);
    if (
      c &&
      !(
        activeWallet.plan?.active &&
        !activeWallet.unverified &&
        activeWallet.plan.benefits.includes("No upsell banners")
      )
    ) {
      exposePricing(c.version, "paywall");
      for (const event of ["paywall_viewed", "credits_upsell_shown"] as const)
        trackPurchase({
          event,
          pricingVersion: c.version,
          feature: quote.action,
          balance: activeWallet.available,
          nextReset: activeWallet.daily_free.resets_at,
        });
    }
    navigate("pack_list");
  }
  function select(sku: string) {
    setSelection(sku);
    setSelectionVersion(c?.version ?? "");
    if (c)
      trackPurchase({
        event: "ui_purchase_option_selected",
        pricingVersion: c.version,
        offerId: sku,
        billingPeriod: surface === "pack_list" ? "one_time" : interval,
      });
  }
  async function checkout() {
    if (!c || !selection || stale || native) return;
    setCheckoutBusy(true);
    setCheckoutError(false);
    trackPurchase({
      event: "ui_checkout_requested",
      pricingVersion: c.version,
      offerId: selection,
      billingPeriod: surface === "pack_list" ? "one_time" : interval,
    });
    try {
      const order = await checkoutAPI.create({
        request_id: crypto.randomUUID(),
        sku: selection,
        pricing_version: c.version,
        platform,
        return_to: "/credits",
        quote_id: recoveryQuoteId,
      });
      recoveryID("order", order.id);
      setOrderId(order.id);
    } catch {
      setCheckoutError(true);
    } finally {
      setCheckoutBusy(false);
    }
  }
  return (
    <section className="pricing" aria-labelledby="pricing-heading">
      <h1 id="pricing-heading" ref={heading} tabIndex={-1}>
        {messages.title}
      </h1>
      {orderId && (
        <CheckoutReturn
          id={orderId}
          onChooseOption={() => {
            recoveryID("order", null);
            setOrderId(undefined);
            navigate("pack_list");
          }}
          onReturn={(order) => {
            if (order?.state === "paid") void walletQuery.refetch();
            recoveryID("order", null);
            setOrderId(undefined);
            if (order?.quote_id) {
              setRecoveryQuoteId(order.quote_id);
              recoveryID("quote", order.quote_id);
            }
            setReview(true);
            navigate("wallet");
          }}
        />
      )}
      <div hidden={!!orderId}>
        {review && (
          <div className="pricing-card quote-panel">
            <p>
              Your saved work is ready. Resume opens review; sending or
              generation needs your explicit approval.
            </p>
            <Button
              onClick={() => {
                setReview(false);
                navigate(recoveryQuoteId ? "quote" : "wallet");
              }}
            >
              Resume action
            </Button>
            <Button variant="quiet" onClick={() => setReview(false)}>
              Not now
            </Button>
          </div>
        )}
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
              <WalletView
                wallet={activeWallet}
                onViewOptions={() => navigate("pack_list")}
                onWait={() => {
                  setWaiting(true);
                  void walletQuery.refetch();
                }}
                trackView={false}
              />
            )}
            {(surface === "pack_list" || surface === "plan_list") && (
              <>
                <h2>Make room for your next idea.</h2>
                <PlanSummary wallet={activeWallet} />
                {activeWallet.plan?.active && (
                  <p role="status">
                    Your funded plan is already active. Manage billing to review
                    renewal options.
                  </p>
                )}
                <div className="pricing-card pricing-credit">
                  <p>
                    Current balance:{" "}
                    {walletQuery.data
                      ? formatCredits(activeWallet.available)
                      : "Not verified"}
                    {walletQuery.data &&
                      activeWallet.unverified &&
                      " · cached, unverified"}
                    {recoveryQuote &&
                      ` · Action needs up to ${formatCredits(recoveryQuote.content_ceiling + recoveryQuote.audio_ceiling)} credits`}
                  </p>
                  <p>Your saved lessons and notes stay available.</p>
                  {recoveryQuote && (
                    <p>
                      Action quote expires:{" "}
                      {date(
                        new Date(recoveryQuote.expires_at),
                        activeWallet.daily_free.timezone,
                        { dateStyle: "medium", timeStyle: "short" },
                      )}
                      . Renew an expired quote and review before resuming.
                    </p>
                  )}
                  <p>
                    Next local reset:{" "}
                    {activeWallet.daily_free.resets_at
                      ? date(
                          new Date(activeWallet.daily_free.resets_at),
                          activeWallet.daily_free.timezone,
                          { dateStyle: "medium", timeStyle: "short" },
                        )
                      : "Connect to confirm"}{" "}
                    · {activeWallet.daily_free.timezone}
                  </p>
                  <p>
                    {recoveryQuote && !recoveryQuote.daily_free_eligible
                      ? "Daily free credits cannot fund this action. "
                      : ""}
                    Daily free credits are eligible for:{" "}
                    {c.daily_free.eligible_actions
                      .map((action) => action.replace(/_/g, " "))
                      .join(", ")}
                    . Spent first; unused daily credits expire at local day end.
                  </p>
                  <p>
                    {native
                      ? "Native store purchases unavailable until verified store billing is configured. No external checkout is offered."
                      : "Permitted channel: web/desktop illustrative preview. Live payment channels are not enabled."}
                  </p>
                </div>
                <Button
                  variant="quiet"
                  onClick={() => {
                    setReview(true);
                    navigate("wallet");
                  }}
                >
                  Back to saved work
                </Button>
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
                            onChange={() => select(p.sku)}
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
                            aria-label={`${p.name} · ${interval}`}
                            checked={selection === p[interval].sku}
                            onChange={() => select(p[interval].sku)}
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
                          {interval === "yearly" && (
                            <p>
                              Monthly equivalent:{" "}
                              {formatMoney(
                                p.yearly.amount_minor / 12,
                                p.yearly.currency,
                              )}{" "}
                              · full yearly charge above
                            </p>
                          )}
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
                {!!activeWallet.source_debt && activeWallet.source_debt > 0 && (
                  <p role="status" className="pricing-debt-note">
                    Paid AI features are paused pending resolution of a payment
                    reversal. Buying credits does not lift the pause; daily free
                    credits remain available.
                  </p>
                )}
                <p>{messages.paid}</p>
                {surface === "plan_list" ? (
                  <p>
                    {interval === "yearly"
                      ? messages.annual
                      : "Billed monthly; credits granted each paid service month. Renews automatically; cancel any time. Taxes are additional."}
                  </p>
                ) : (
                  <p>
                    One-time purchase. Taxes shown at checkout. Web refunds
                    cover the unused paid portion, subject to mandatory law;
                    store refunds use the store process.
                  </p>
                )}
                {surface === "plan_list" && (
                  <p>
                    Refund requests follow the billing channel: the unused paid
                    portion for web, subject to mandatory law; store purchases
                    use the store refund process. Issued credits remain after
                    cancellation.
                  </p>
                )}
                <p role="status">{messages.readonly}</p>
                {checkoutError && (
                  <p role="alert">
                    Checkout unavailable. Refresh prices and review your
                    selection; your work is saved.
                  </p>
                )}
                {checkoutBusy && (
                  <p role="status">Preparing illustrative checkout…</p>
                )}
                <Button
                  disabled={
                    !selection ||
                    stale ||
                    native ||
                    checkoutBusy ||
                    query.isFetching ||
                    (surface === "plan_list" && !!activeWallet.plan?.active)
                  }
                  aria-describedby="sale-requirements"
                  onClick={() => void checkout()}
                >
                  Preview checkout
                </Button>
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
                <Button
                  variant="quiet"
                  onClick={() => {
                    setWaiting(true);
                    navigate("wallet");
                    void walletQuery.refetch();
                  }}
                >
                  Wait for daily reset
                </Button>
              </>
            )}
            {waiting && surface === "wallet" && (
              <p role="status">
                {walletQuery.isFetching
                  ? "Confirming daily reset…"
                  : walletQuery.isError
                    ? "Reset awaiting sync. Connect and check again; your work is saved."
                    : walletQuery.data
                      ? "Daily reset checked with the server. Your work is saved; no action was sent."
                      : "Sign in to confirm your daily reset. Your work is saved."}
              </p>
            )}
            {(quoteVisited || recoveryQuoteId) && (
              <div hidden={surface !== "quote"}>
                <h2>Create a course · price guide</h2>
                <QuotePanel
                  narrationCharacters={c.narration.characters}
                  recoveryQuoteId={recoveryQuoteId}
                  onInsufficient={showRecovery}
                  onQuoteRecovered={setRecoveryQuote}
                />
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
                      {formatCredits(c.actions.lesson_content.credits)} content
                      + {formatCredits(c.narration.credits)} narration for{" "}
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
              </div>
            )}
          </>
        )}
      </div>
    </section>
  );
}
