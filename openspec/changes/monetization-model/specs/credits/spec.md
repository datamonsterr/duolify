## MODIFIED Requirements

### Requirement: Prices from config
The API MUST compute each AI action price from an immutable versioned server-side catalogue and reported `Usage`, including provider/model, context bracket, uncached input, cache writes, cache reads, output and newly synthesized narration characters. The catalogue MUST define currency, service minimums, conversion rates, character rational rates, rounding, request bounds, quote ceilings, effective date and external product mappings; commercial values MUST NOT be hard-coded in client or Engine logic. Existing ledger entries and accepted reservations MUST retain their original price version. A standard successful token action SHALL settle the lesser of the approved ceiling and the greater of its configured service minimum or rounded usage price; narration SHALL settle the aggregate new-character price rounded up once. Failed components MUST NOT be charged. Provider cost MUST be recorded separately from customer Credits, including platform-funded retries.

#### Scenario: Price update during generation
- **WHEN** an accepted reservation is running when a new price table becomes effective
- **THEN** settlement uses the reservation's original version and cannot exceed its approved ceiling

#### Scenario: Unknown model rate
- **WHEN** the configured provider/model or context bracket has no validated price
- **THEN** the API rejects the quote and MUST NOT start provider work at an invented price

#### Scenario: Narration cache hit
- **WHEN** requested narration already exists for the same text, voice, model and settings hash
- **THEN** no new narration character charge is added and the existing artifact is returned

### Requirement: Top-ups
Learners SHALL add paid Credits through the verified billing channel permitted for their platform and region. Desktop/web SHALL support server-created Stripe Checkout sessions from catalogue SKUs. Mobile store builds MUST use native digital-goods billing unless an explicitly configured regional/program exception permits an external checkout flow. The API MUST bind each order to an authenticated Learner and expected SKU, amount, currency and environment. It MUST append a `purchase` entry only after verifying server-side paid state and provider authenticity. Stripe fulfillment MUST support delayed payments and MUST NOT treat a redirect or unpaid completed session as proof of payment. Event IDs and business payment IDs MUST both be deduplicated transactionally with ledger mutation and the financial outbox.

#### Scenario: Different events for one payment
- **WHEN** a paid session is reported through multiple valid events with different event IDs
- **THEN** exactly one purchase entry exists for that provider payment and the Learner receives its pack once

#### Scenario: Delayed payment
- **WHEN** a Checkout session completes with payment pending
- **THEN** no Credits are granted until a verified asynchronous success or canonical paid-state check confirms payment

#### Scenario: Invalid webhook
- **WHEN** a webhook signature, environment, currency or order binding is invalid
- **THEN** the event cannot change a balance and the failure is recorded for reconciliation

#### Scenario: External checkout not permitted
- **WHEN** a store-distributed app has no configured exception for the Learner's region
- **THEN** it offers permitted native billing and exposes no external purchase link

## ADDED Requirements

### Requirement: Monetization launch catalogue
The initial proposed USD catalogue SHALL define a Credit list value of USD 0.01 and a protected AI budget of USD 0.0015 per spent Credit. Standard prices SHALL be Course preview 25 Credits, Course outline 55, Lesson content including narration text, test and retry pool 140, short Tutor chat 5, long Tutor chat 8 and Quick note enrichment 2. Narration SHALL use 50/1,440 Credits per newly synthesized character, rounded up once for a request, making a standard 1,440-character narrated Lesson 190 Credits including content. Catalogue bounds SHALL match the costed envelopes in `docs/business/monetization.md`; larger requests MUST obtain a fresh disclosed quote. One-time packs SHALL be USD 5 for 400 Credits, USD 15 for 1,350 and USD 40 for 4,000. Replays of delivered artifacts and retry-pool reuse MUST NOT incur another AI charge. Future catalogue versions MUST pass a paid AI gross-margin gate of at least 70% after configured channel fees and refund allowance, using current protected provider costs and full redemption.

#### Scenario: Standard narrated Lesson quote
- **WHEN** the Learner requests a standard new Lesson with 1,440 new narration characters
- **THEN** the API displays and reserves 140 content Credits plus 50 audio Credits before generation and records the catalogue version

#### Scenario: Margin gate fails
- **WHEN** a proposed SKU discount or provider-price update makes its configured paid AI gross margin lower than 70%
- **THEN** activation fails for new sales until its economics or evaluated provider selection is corrected, without rewriting existing paid terms

### Requirement: Daily free catalogue sizing
The daily free grant SHALL initially be 46 Credits, sufficient for three Quick note enrichments plus either eight short Tutor messages or five long Tutor messages within the published envelopes. This bucket MUST expire at the end of the Learner's local day and MUST be spent before paid Credits on eligible chat/notes. Daily free Credits MUST NOT fund custom Course preview, Course outline, Lesson generation or narration under the launch catalogue. The client MUST disclose message size class and bucket eligibility before approval. The same daily grant SHALL apply to subscribers. No effort, XP, Streak, Friend streak or test result SHALL generate Credits; Streak freezes remain earned by effort and automatically applied. Grant uniqueness, bucket semantics and reward removal SHALL be provided by the prerequisite `product-rules-v1` change.

#### Scenario: Short daily allowance
- **WHEN** a Learner spends the new daily grant on three standard notes and eight short messages
- **THEN** the total is 46 Credits, no paid Credits are required and the free balance is exhausted

#### Scenario: Long daily allowance
- **WHEN** a Learner spends the new daily grant on three standard notes and five long messages
- **THEN** the total is 46 Credits and the free allowance covers all those actions

#### Scenario: Generation with only free balance
- **WHEN** a Learner with daily free Credits and no paid Credits requests a custom Course outline
- **THEN** the API returns insufficient eligible Credits and offers the permitted purchase flow without starting generation

#### Scenario: Expiry while reserved
- **WHEN** reserved daily Credits cross their expiry before a failed request releases them
- **THEN** the expired allocation cannot become spendable on the next day and the new daily grant remains independent

### Requirement: Paid credit provenance and rollover
Paid purchase and subscription-grant Credits MUST retain source payment, provider, SKU, price version and service-period provenance. Paid Credits SHALL have unlimited rollover without expiry or a balance cap, including after cancellation or downgrade. Spending MUST use eligible daily Credits first and then oldest eligible paid Credits atomically. The API MUST maintain an outstanding-paid-credit liability view and cost reserve at the configured protected cost per Credit; analytics or cache loss MUST NOT erase that obligation.

#### Scenario: Cancel with unused paid Credits
- **WHEN** a subscription ends with unused issued subscription or pack Credits
- **THEN** those Credits remain usable under their preserved terms and are not expired or converted to daily Credits

#### Scenario: Concurrent spending
- **WHEN** two requests attempt to reserve the same remaining paid balance
- **THEN** their combined allocations cannot exceed the spendable balance

### Requirement: Financial reversals and reconciliation
Refunds and disputes MUST append signed compensating ledger entries with the original payment reference and unique provider reversal key; they MUST NOT edit past purchases or spends. A reversal MUST reduce only the attributable unspent source bucket and affected future allowance. A forced reversal exceeding that bucket MUST preserve prior spends, record a non-spendable source deficit and block new paid AI work pending resolution without taking unrelated Credits. Cumulative monetary reversals MUST NOT exceed the provider-confirmed refundable payment amount. Daily reconciliation MUST compare provider paid/refunded state, fulfilled orders and local ledger/grant references, repair missing authorized operations idempotently and alert on unresolved differences.

#### Scenario: Duplicate partial refund
- **WHEN** a provider repeats a partial refund notification
- **THEN** one monetary refund and one attributable credit reversal are recorded, with no second deduction

#### Scenario: Forced refund after spending
- **WHEN** a confirmed chargeback exceeds the original purchase bucket's unspent Credits
- **THEN** the source deficit is recorded, unrelated balances remain intact and new paid spending is blocked pending resolution

#### Scenario: Missed payment notification
- **WHEN** reconciliation finds a verified paid order with no local fulfillment
- **THEN** it appends the missing purchase exactly once using the original business operation key

### Requirement: Monetization cost and model reliability
The API MUST enforce configured USD budgets independently of Credit balances and PostHog delivery: initially USD 0.08 per free Learner/local day, USD 5 per paid Learner/local day and USD 0.50 per provider job. Token, context, character, timeout, retry and concurrency limits MUST also be configured and enforced. Standard Tutor prompts MUST stay below the configured 48,000-token limit; full Course context access MUST use a costed retrieval/summary strategy rather than silent truncation. Golden set cases MUST cover usage envelopes, cache writes/misses, retry variants, multilingual narration alignment, fallback, price changes, timeouts and caps at a pinned Hugging Face revision. Model/voice or prompt changes MUST pass `evals/thresholds.toml`, `mise run engine:eval` and focused live evaluations before release, with no cost or ledger safety violations. Fallback IDs MUST resolve in the provider catalogue; at most one bounded repair/fallback SHALL follow the primary attempt. All-provider failure MUST release the reservation, and a successful text Lesson with failed audio MUST charge only its delivered component.

#### Scenario: Expensive fallback
- **WHEN** a fallback would exceed the accepted quote or provider USD cap
- **THEN** work stops without an undisclosed charge and any undelivered component reservation is released

#### Scenario: Audio alignment failure
- **WHEN** content generation succeeds but narration fails alignment checks
- **THEN** the text Lesson remains usable, audio is uncharged or refunded and a retry reuses the existing content hash

#### Scenario: Analytics unavailable
- **WHEN** PostHog is unavailable or the Learner has opted out
- **THEN** server-side operational cost caps and ledger reconciliation continue to operate

### Requirement: Monetization credits analytics
The API/client SHALL emit `paywall_viewed` with placement, feature, balance, plan_id and price_table_version; `checkout_started` with sku_id, channel, currency and amount_minor; `credits_exhausted` with feature, bucket, balance and estimate; `credits_refunded` with amount, payment_reference and reason_code; `price_table_activated` with old_version and new_version; `ai_cost_budget_alert` with scope, actual_usd and budget_usd; and `payment_reconciliation_failed` with channel and reason_code. These events MUST extend the canonical analytics catalogue before emitters ship and MUST follow its common properties, privacy and opt-out rules. Existing Credits events and `$ai_generation` MUST remain supported with price version, actual provider/model, token/cache classes, character count, fallback, latency and USD cost where relevant. Financial events MUST be queued idempotently from the committing ledger transaction. Initial alerts SHALL fire at 80% and 100% of a USD 50 platform daily budget, a 20% rolling seven-day cost-per-active-Learner increase, cache hits below 60%, or paid AI margin below 70%; thresholds MUST be configurable.

#### Scenario: Exhaustion funnel
- **WHEN** the API rejects an action for insufficient eligible Credits
- **THEN** it emits the existing insufficient event and `credits_exhausted` with the attempted feature and bucket, while the client records any resulting paywall view without content text

#### Scenario: Replayed financial event
- **WHEN** the same ledger operation is reprocessed
- **THEN** its financial outbox event is not created a second time

### Requirement: Monetization financial backup and retention
Postgres SHALL retain payment provenance, grant/reversal business keys, quote/catalogue versions and outstanding-credit liabilities with the append-only ledger for at least seven years or longer where required by law. These records MUST be covered by encrypted nightly backups, 35-day snapshot retention and PITR where available. Redacted raw webhook payloads SHALL expire after 30 days and derived analytics after 90 days. Account deletion MUST remove content and pseudonymize financial identity except legally retained fields. Quarterly restore drills MUST verify balances, bucket expiry, dedupe keys, liabilities and provider reconciliation; deletion tombstones MUST be applied after restore. Snapshot rotation MUST NOT delete the live historical ledger or catalogue versions needed to explain it.

#### Scenario: Restore and webhook replay
- **WHEN** a financial backup is restored and verified provider events are replayed
- **THEN** balances and liabilities reconcile to ledger sums and no payment, grant or refund is applied twice

#### Scenario: Deleted Learner in old backup
- **WHEN** a restored snapshot contains content previously deleted by a Learner
- **THEN** deletion tombstones are reapplied before serving restored data and required financial records retain only permitted identity fields
