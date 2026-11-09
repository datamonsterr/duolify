## Context

E12 supplies the economic contract consumed by E7 Credits and E14 Narration. The business evidence, assumptions, equations and scenario results live in [monetization.md](../../../docs/business/monetization.md); [model.csv](../../../docs/business/model.csv) contains computed monthly outputs. Rates were checked on 2026-11-08. This change does not implement billing or change files outside its documentation scope.

`product-rules-v1` must land before this change is archived. That dependency removes effort-based credit rewards and defines canonical daily-grant behavior. Avoid duplicate removal deltas here. Any overlap in requirement text must be rebased after that dependency; validation alone does not resolve semantic conflicts.

## Goals and non-goals

- Price AI with a predictable quote, report actual provider cost, and preserve at least 70% paid AI margin under the documented baseline/adverse channel assumptions.
- Fund the fixed daily notes/chat allowance using paid packs and monthly/yearly plans.
- Make grants, settlements, refunds and restores auditable and idempotent across payment channels.
- Keep prices, limits and entitlements in versioned configuration.
- Do not implement billing, create provider products, process real payments, publish a price page, or claim accounting/tax approval in this change. Family plans, unlimited AI and effort-based credit rewards are excluded.

## Catalogue decisions

Initial proposed catalogue version: `usd-launch-2026-11-08`. Runtime publication requires owner adoption and the tests in tasks.md; this documentation is not a live price activation.

| Item | Proposed config value |
|---|---|
| Credit list value / protected cost ceiling | USD 0.01 / USD 0.0015 |
| Daily free bucket | 46 Credits; notes and Tutor chat only; expires at local-day end |
| Standard action prices | Preview 25; outline 55; Lesson text/test/retry pool 140; short chat 5; long chat 8; note 2 |
| Narration | Rational rate 50/1,440 Credits per newly synthesized character, integer ceiling once per request; standard audio 50, complete Lesson 190 |
| Packs | USD 5/400 Credits; USD 15/1,350; USD 40/4,000 |
| Learn | USD 9.99 monthly or 96 yearly; 600 Credits/service month |
| Plus | USD 19.99 monthly or 192 yearly; 1,600 Credits/service month |
| Pro | USD 39.99 monthly or 384 yearly; 3,600 Credits/service month |
| Paid rollover | Unlimited, no expiry, including after cancellation; retain source-payment provenance |

Store currencies/price points are explicit catalogue entries; a client cannot perform currency conversion or send an arbitrary amount. Public prices state interval, monthly grant, rollover, tax treatment and channel. If the same USD price point is unavailable on a store, configure its actual price and rerun the margin test before selling it. Display store-localized prices from that verified product, not a hard-coded USD equivalent.

Each immutable catalogue version contains `effective_at`, currency and integer minor-unit SKU amounts, channel/region eligibility, external price/product IDs, plan entitlements, action minimums, token rates by provider/model/context bracket/cache class, character rates, output/input caps, USD spend budgets and rounding policy. Use fixed-point decimal money and integer Credits. A quote snapshots its version, components, ceilings, expiry, input bounds and permitted model/voice. Honor accepted terms for reservations and paid service periods; new versions affect new quotes and future renewals only after required notices/consent. Historical versions remain reconstructible from financial records and backups.

Initial quotes remain acceptable for five minutes, a proposed UX default aligned with short sessions rather than provider cache lifetime. A reservation created before expiry remains pinned through completion, even if the catalogue changes. Catalogue validation must reject nonpositive rates, missing model aliases, insufficient free allowance, unknown product IDs, invalid entitlements and SKUs below the margin target. Stop selling a broken SKU while keeping prior paid terms usable within safety budgets.

## Usage and charging

The API owns price quotes and the ledger; the Engine reports usage and artifacts. The Engine cannot grant Credits or trust a client-provided price. Store `quote_id`, `price_table_version`, model/provider, token classes, audio character count, bucket allocations and payment provenance with every financial operation.

For token actions settle `min(approved ceiling, max(configured service minimum, ceil(actual billable provider USD / configured cost-per-Credit ceiling)))`; use the configured rational character rate for narration. Standard envelopes reserve their displayed minimum and ceiling, so successful standard delivery has a predictable charge. Actual provider expense remains recorded separately and includes platform-funded repair work. Never bill a second customer action for a retry, cache hit or repeated delivery. Out-of-envelope content requires a new quote; a job that cannot finish safely releases its reservation.

Reserve separate Lesson content and narration components. Debit only newly synthesized characters: a hash of text, voice, model and settings identifies paid audio. Audio failure must not discard a successful text Lesson or charge for missing audio. Retry pool reuse and all cached replays incur no AI charge. Initial generation of the next Lesson still requires the previous Lesson test pass.

Grant daily Credits once per Learner/local date, snapshotting IANA timezone and expiry. An elapsed-time guard prevents timezone cycling; changes take effect after 24 hours. Daily Credits may only fund chat/notes; custom previews, outlines, Lessons and narration draw paid balances. Preserve the exact 3-notes-plus-8-short-or-5-long promise. Reserve bucket allocations atomically to prevent concurrent overspend. A reservation that crosses daily expiry may settle already reserved work; releasing expired daily Credits never makes them spendable on the next day. New-day grants are independent.

## Subscription lifecycle and entitlements

Plans are individual. Learn enables offline library downloads, no upsell banners and learning-history export. Plus adds priority generation, advanced analytics and curated voices with no voice-license surcharge. Pro adds custom analytics export and a higher generation concurrency limit. Cached replay stays universal. No entitlement grants effort Credits or sells Streak freezes; freezes remain effort-earned and auto-applied.

An entitlement record has Learner, provider subscription ID, plan/version, status, paid-through timestamp and version. Derive it from verified payment state, persist it in Postgres and cache only derived views. An outbox event invalidates caches. Restarting a worker cannot create or extend an entitlement.

Monthly subscriptions grant once per verified paid invoice/service period. Annual subscriptions grant the same monthly allowance in each service month, using a durable scheduler keyed by provider subscription, paid term and month index. The first grant is issued atomically with paid activation. The other monthly grants require the annual term still to be funded and not refunded; cancellation of auto-renew does not revoke a funded term. Month-end anniversaries clamp to the last valid date, and grants never move beyond the paid-through timestamp. Worker catch-up may issue missed funded months once each. Turning auto-renew off preserves paid benefits through term end and all previously granted Credits forever.

Do not issue a grant from `checkout.session.completed` and again from `invoice.paid` for the same subscription. The checkout event links the subscription; verified paid invoice state authorizes activation and the grant. No grants on trial/unpaid invoices. Launch has no automatic free trial. Provider-confirmed paid-through access remains until expiry during dunning; no unfunded allowance is issued. A refunded/charged-back annual term cancels future scheduled grants.

For Stripe, launch plan changes take effect at the next renewal with no interim proration; show the effective date. For stores, honor mandatory immediate upgrade/proration semantics, reconcile the provider's effective period and issue only the net newly purchased allowance according to a versioned mapping. Never pretend all stores permit delayed upgrades. Prevent overlapping new subscriptions in the app, show the existing channel's management link, and reconcile accidental cross-channel duplicates without double allowances.

## Checkout, webhook processing and reconciliation

Desktop/web uses Stripe Checkout with a server-selected catalogue SKU and an authenticated Learner reference. Store-distributed builds use StoreKit/Play Billing for digital purchases unless a documented, configured exception applies to that exact channel, territory and program. A policy matrix records eligibility, consent/disclosure requirements, effective dates and commission class; default unknown combinations to no external purchase link. Access to existing cross-platform purchases must respect the same verified entitlement.

Webhook flow:

1. Verify signature against the raw body, timestamp/replay window and provider account/environment. Reject invalid events before persistence or ledger mutation.
2. Persist an inbox event with unique `(provider, event_id)`. Acknowledge only after durable receipt; process asynchronously with bounded retries and a dead-letter/replay path.
3. Resolve canonical order/invoice/refund state from the provider when events are stale or out of order. Verify product, amount, currency, environment, paid status and Learner binding. Never trust metadata alone or success redirects.
4. Within one Postgres transaction, claim the business operation key, append the ledger entry, update order/subscription/entitlement state and enqueue an outbox event. Pack key is `(provider, payment_id)`; grant key is `(provider, subscription_id, funded_term, service_month)`; reversal key is `(provider, refund_or_dispute_id, operation)`. Unique event IDs alone cannot prevent duplicate grants from different event types.
5. Reconcile local paid invoices/grants/refunds against provider records daily and after a restore. Delayed Stripe payments grant only after paid fulfillment (`checkout.session.async_payment_succeeded` or confirmed paid state); failed/abandoned sessions grant nothing. Subscription grants require a verified paid invoice and expected service period.

Apple signed transaction/server notifications and Google purchase-token verification plus real-time notifications use equivalent inbox/business-key semantics. Mobile restore rebuilds entitlements from provider transactions; it never manufactures consumable Credits or repeats a grant already in the ledger. Bind a purchase to one Learner and reject attempts to replay it into another account.

## Refunds and disputes

Publish a request path, channel ownership and legally required withdrawal/refund rights before checkout. For web purchases, propose refunding the unused paid service/credit portion on request; mandatory law overrides this policy. Stores adjudicate their purchases and their verified decisions are authoritative. Refunds are money movements plus signed compensating ledger entries, never edits to the original purchase/spend. A refund reduces remaining Credits attributable to that purchase and revokes only the affected funded future allowance/benefits; do not confiscate unrelated pack balances or daily grants.

If a forced full refund/chargeback exceeds the unspent source bucket, record the deficit as non-spendable debt in that source bucket and block new paid AI spending pending resolution; do not delete prior spends or reclaim unrelated Credits. Free daily entitlement remains available subject to fraud review. Explicitly approved goodwill refunds may waive that debt through a separate auditable adjustment. Repeated dispute/refund notifications must not double-reverse money, Credits or analytics. Partial refunds record their exact monetary/credit allocation; cumulative reversal cannot exceed the original refundable amount. Processing fees are an operating expense and are not assumed returned.

## Safety, analytics and data

Use the business document's proposed limits: USD 0.08/free Learner/local day, USD 5/paid Learner/local day, USD 0.50/request, bounded token/context/time/concurrency limits and platform budget alerts. Safety counters are independent of PostHog delivery and opt-out. Honor queued legitimate free work; if systemic cost threatens runway, stop new admissions or pause an affected feature transparently rather than silently shrinking the daily promise.

Both capability deltas define event names/properties. Before shipping emitters, update the canonical analytics catalogue in its own authorized implementation change. Financial outbox and ledger events are transactional and idempotent; analytics remains non-blocking, content-free and opt-out aware. Alert on actual cost, cache behavior, price drift and reconciliation failures; keep Postgres operational audit independent of analytics.

Retain ledger, payment references, grant keys, signed adjustments and catalogue versions for at least seven years or longer where legally required. Encrypt nightly backups, retain snapshots 35 days, use PITR where available, and perform quarterly restore/replay drills. Redacted raw webhook payloads expire after 30 days, derived PostHog events after 90 days. Delete account content and pseudonymize legally retained financial identity; apply deletion tombstones when restoring snapshots. Financial metadata must never depend on an object-store cache or PostHog as its sole copy. Back up monthly grant schedules, entitlement history and outstanding-credit liability as well as balances.

## Alternatives, risks and rollout

- Unlimited AI subscriptions conceal narration and context cost; use explicit monthly Credits and quotes.
- Expiring paid balances improve paper margins but conflict with Apple purchased-credit rules and complicate cross-device balances; keep them non-expiring and reserve their service liability.
- Fixed feature prices without usage/cost reporting hide provider inflation; use service minimums plus bounded usage pricing and immutable quote versions.
- A store-wide external Stripe link can violate regional policy; gate it using verified eligibility and default to native billing.
- The configured OpenRouter hyphenated IDs are not in the fetched model catalogue. Correct and smoke-test decimal aliases in a separately authorized implementation change, then run pinned Golden set evaluations. Do not assume a routing provider guarantees a distinct underlying model provider.
- Public official Max pricing was not verifiable; disclose that gap rather than building the plan on historical competitor prices.

Rollout order: merge/rebase on `product-rules-v1`; adopt the commercial assumptions; write e2e, integration and unit tests; implement catalogue/ledger primitives; implement Stripe and native channel verification; enable budget dashboards and backups; pass Golden set/live provider checks and restore/replay drills; enable a bounded pilot. Catalogue activation and payment-product creation are future implementation tasks, not completed work in this change.
