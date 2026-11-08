## Documentation deliverable

- [x] Read the glossary, ADRs, E12 scope, model config and current Credits specification.
- [x] Retrieve dated official provider, payment, store and positioning evidence; disclose unavailable prices and invalid fallback IDs.
- [x] Document explicit assumptions, unit costs, credit prices, packs, subscriptions, rollover, sensitivities and owner questions.
- [x] Produce twelve computed monthly rows for each scenario in `docs/business/model.csv` and preserve selected OpenRouter API prices.
- [x] Draft the proposal, design and Credits/Subscriptions deltas in this change only.
- [x] Run `openspec validate monetization-model --strict` and the focused mise task for all strict validation; independently verify all CSV equations, action budgets, SKU margins and local links; commit the documentation.

## Implementation prerequisites

- [ ] Merge/rebase on `product-rules-v1` before archive; resolve any overlapping Credits requirement text and remove legacy effort-credit behavior through that dependency.
- [ ] Adopt target merchant/markets, budgets, catalogue and free discovery policy; recheck taxes, store eligibility, localized prices and refund obligations.
- [ ] Add the event contracts from these deltas to the canonical analytics catalogue in an authorized follow-up change before shipping emitters.

## E2E tests first

- [ ] Write failing e2e flows for free daily use: three notes plus eight short or five long messages, mixed messages, local expiry, timezone travel and concurrent reservation; assert no effort-based Credit grants.
- [ ] Write failing e2e flows for quoted custom preview, outline, gated Lesson plus narration, partial audio failure/refund, cached replay and exhaustion/top-up.
- [ ] Write failing e2e flows for each pack, monthly/yearly paid activation, anniversary grants, unlimited rollover after cancellation, renewal failure, plan changes, cross-channel duplicate subscription and purchase restoration.
- [ ] Write failing e2e flows for delayed Stripe payments, invalid signatures, duplicate/out-of-order event types, partial/full refunds, chargeback deficits and provider reconciliation; check financial events with a PostHog test double.
- [ ] Write failing platform/region flows for native billing, a permitted external checkout exception and a disallowed external purchase link.

## Integration tests second

- [ ] Test Postgres unique business keys, concurrent balance/bucket allocation, inbox/ledger/entitlement/outbox atomicity, job retry and cache invalidation against real Postgres.
- [ ] Test annual month-end scheduling, worker outage/catch-up, refunded funded terms, stale cancellation messages and paid-through enforcement against provider fixtures.
- [ ] Test signed Stripe and native notification verification, paid-state lookup and sandbox fulfillment/refunds without double grants.
- [ ] Test daily provider reconciliation, dead-letter replay, analytics outage/opt-out independence and nightly backup/quarterly restore checks, including deletion tombstones and outstanding-credit liabilities.

## Unit tests third

- [ ] Test fixed-point pricing, character rational rates, service minimums, integer rounding, quote expiry/version pinning, cache classes, long-context bracket, channel fee/margin gates and malformed catalogue rejection.
- [ ] Test grant eligibility, no reward source, source-bucket refund allocation, negative refund debt, entitlement state transitions, channel policy lookup and content-free analytics properties.
- [ ] Test deadline/output/USD cap enforcement, concurrency/abuse controls, duplicate narration hashes and failure reservation release.

## Implementation after tests

- [ ] Implement versioned server-side catalogue config and immutable quote snapshots; do not hard-code commercial values in API, Engine or client logic.
- [ ] Implement daily eligible buckets, paid provenance, non-expiring rollover and append-only reserve/settle/refund operations; keep services stateless.
- [ ] Implement Stripe Checkout, durable verified webhook inbox, business-key fulfillment, annual scheduler, entitlements, refunds/disputes, outbox and daily reconciliation.
- [ ] Implement native billing and restore by region/program matrix, provider-mandated upgrades, management/cancellation links and duplicate-subscription prevention.
- [ ] Implement the price breakdown, free allowance, plan benefits, localized checkout, paid-through and rollover UI; no undisclosed charges or universal external checkout links.
- [ ] Implement all declared PostHog events, budget/cache/margin alerts and independent operational spend counters; add backups, retention/deletion jobs and quarterly restore/replay runbook.

## AI reliability and release

- [ ] Verify current primary IDs and correct OpenRouter decimal fallback IDs in authorized config work; record the checked provider rates and price version.
- [ ] Add pinned Hugging Face Golden set cases for usage envelopes, full-course context retrieval, multilingual narration text/alignment, retry-pool completeness, cache misses, fallback, timeout, price changes and cost limits.
- [ ] Require `evals/thresholds.toml` pass thresholds, no cost/format/ledger safety violations, `mise run engine:eval` and focused live evaluations via `mise run engine:eval-live`; validate selected voice/model alignment before launch.
- [ ] Run only focused area mise checks for implementation, payment sandbox tests and restore reconciliation; repeat both strict OpenSpec validations before adoption and archive only after implementation is complete.
