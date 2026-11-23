# credits Specification

## Purpose
Charge for AI features with prepaid Credits kept in an append-only ledger. Credits are reserved before an AI call and settled with actual usage. Learners receive free Daily credits and can top up. Credits are never granted as a reward for effort.

## Requirements

### Requirement: Append-only ledger
Credits SHALL be stored as entries in `credit_entries` with learner, amount, kind (`daily_grant`, `check_in_grant`, `check_in_bonus`, `purchase`, `subscription_grant`, `spend`, `reserve`, `release`, `refund`, `expire`) and reference. Entries MUST NOT be updated or deleted. Balance MUST be derived from entries and MAY be cached. Daily free credits, purchased Credits and subscription Credits MUST be separable by kind. There MUST be no `reward` kind.

#### Scenario: Correction
- **WHEN** an operator corrects a wrong charge
- **THEN** a new `refund` entry is appended and the old entry is untouched

#### Scenario: Cache lost
- **WHEN** the balance cache is empty
- **THEN** the balance is recomputed from entries

### Requirement: Reserve and settle
Before an Engine call the API MUST reserve an estimate. After the call the API MUST settle the actual cost from `Usage` and release the remainder. A failed call MUST release the full reservation. Reservation, settlement and release MUST be atomic with the job state.

#### Scenario: Successful call
- **WHEN** the Engine returns `Usage`
- **THEN** a `spend` entry equals the price of the usage and a `release` entry returns the unused estimate

#### Scenario: Engine fails
- **WHEN** all providers fail
- **THEN** the reservation is released and the Learner is not charged

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

### Requirement: Insufficient balance
When the spendable balance is lower than the estimate, the API SHALL refuse the AI request with `insufficient_credits`, MUST NOT call the Engine, and MUST return the Learner's next Daily free credits grant time and the available top-up options. The client MUST show an upsell screen with those options. No AI feature MAY fail silently for lack of Credits. Spendable balance MUST exclude expired Daily free credits.

#### Scenario: Low balance
- **WHEN** a Learner with zero spendable Credits opens Tutor chat and sends a message
- **THEN** the request is refused and the upsell screen is shown

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

### Requirement: Credits analytics events
The system SHALL emit `credits_reserved`, `credits_settled`, `credits_released`, `credits_insufficient`, `credits_purchased`, `credits_purchase_failed`, `daily_credits_granted`, `daily_credits_expired` and `credits_upsell_shown` with properties from the `analytics` spec.

#### Scenario: Settled
- **WHEN** a reservation is settled
- **THEN** `credits_settled` is emitted with `feature`, `amount`, `model` and `input_tokens` and `output_tokens`

#### Scenario: Daily grant
- **WHEN** the daily grant is written for a Learner
- **THEN** `daily_credits_granted` is emitted once for that local day with `amount`

### Requirement: Credits data backup and retention
The ledger MUST be stored in Postgres and covered by the nightly dump and by point-in-time recovery where available. Ledger entries MUST be kept at least 7 years, or as the law for payment records requires. After a restore, balances MUST match the sum of entries.

#### Scenario: Restore drill
- **WHEN** the restore drill finishes
- **THEN** a check query confirms derived balance equals the sum of restored entries for every Learner

### Requirement: Cost reliability
The API MUST enforce a per-request, per-day and per-learner spend cap from config, and MUST alert when platform AI cost reaches 80% or 100% of the platform daily budget (initially USD 50), when rolling seven-day cost per active Learner increases by +20%, when cache hit rate falls below 60%, or when paid AI margin falls below 70%. Alert notifications MUST be deduplicated across notification windows, and operational spend counters MUST continue updating independently when analytics is unavailable or a learner opts out.

See `Monetization cost and model reliability` for related detailed requirements.

#### Scenario: Platform budget alert at 80%
- **WHEN** cumulative daily platform provider spend reaches 80% of the daily platform budget
- **THEN** a deduplicated alert is recorded and emitted to analytics if enabled

#### Scenario: Seven-day learner cost increase
- **WHEN** rolling seven-day cost per active learner increases by at least 20% compared to the prior period
- **THEN** an alert is triggered with deduplication

#### Scenario: Cache hit rate degradation
- **WHEN** cache hit rate falls below 60% over the evaluation window
- **THEN** a cache efficiency alarm is recorded and deduplicated

#### Scenario: Paid margin degradation
- **WHEN** paid AI gross margin falls below 70% over the evaluation window
- **THEN** a margin alarm is recorded and deduplicated

### Requirement: Daily free credits
Every Learner SHALL receive one Daily free credits grant (kind `daily_grant`) per local day, once, idempotent by Learner and local date. The grant amount MUST come from config and MUST be sized to cover 3 Quick notes and 5 to 8 Tutor chat messages: at least 3 Quick note enrichments plus 5 long, context-heavy Tutor chat messages, and up to 8 short messages. A config test MUST check this sizing against the per-feature unit costs. Unit costs and the numeric amount are defined by the monetization change (E12); this spec defines no prices. Unspent Daily free credits MUST expire at the end of the Learner's local day, using the stored time zone, through an `expire` entry for the unspent amount. A Daily free credits grant MUST NOT carry over.

#### Scenario: First action of the day
- **WHEN** a Learner opens the app on a new local day
- **THEN** one `daily_grant` entry exists for that date and a second request on the same date adds none

#### Scenario: End of day
- **WHEN** the Learner's local day ends with unspent Daily free credits
- **THEN** an `expire` entry removes the unspent amount and `daily_credits_expired` is emitted

#### Scenario: Reservation across midnight
- **WHEN** a reservation was made from Daily free credits before the day end
- **THEN** it settles against them and only the unreserved remainder expires

### Requirement: Spend order
The API MUST spend Daily free credits before any other Credits. When a charge exceeds the Daily free credits left, the remainder MUST come from subscription Credits and then purchased Credits, in the order set by the monetization change. Each `reserve`, `spend` and `release` entry MUST reference the kind it draws from.

#### Scenario: Mixed balances
- **WHEN** a Learner has Daily free credits and purchased Credits and sends a Tutor chat message
- **THEN** the Daily free credits are spent first and purchased Credits only for the remainder

### Requirement: No credit rewards for effort
The system MUST NOT grant Credits as a reward for effort, including Streak milestones, Friend streak milestones and perfect Lesson tests. Effort is rewarded with XP, badges and Streak freezes.

#### Scenario: Streak milestone
- **WHEN** a Streak reaches a milestone
- **THEN** no ledger entry is appended

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

### Requirement: Credits read the Pricing config
Quotes, reservations, settlements, Daily free credits grants, pack purchases and Subscription grants SHALL take every amount from the active Pricing config version defined in the `pricing-config` capability, SHALL store that version id on each resulting ledger entry, and MUST NOT use any other source for prices. The catalogue named in `monetization-model` is realized by the Pricing config.

#### Scenario: Daily grant after price change
- **WHEN** a new version lowers Daily free credits and activates at midnight UTC
- **THEN** grants made after activation use the new amount, the grant entry names the new version, and earlier grants are unchanged

#### Scenario: Reservation keeps its version
- **WHEN** a version changes between reservation and settlement
- **THEN** settlement uses the version stored on the reservation

### Requirement: Durable versioned provider jobs
The API SHALL reserve eligible source allocations and USD capacity with one idempotent job transaction before provider execution. Only authenticated owners MAY reserve accepted, unexpired quotes. Pricing config SHALL supply a five-minute TTL, service minimum, token/character/time/USD bounds and plan concurrency. Workers SHALL enforce API-derived bounds and return content-free provider/model/cache/character/fallback/attempt Usage. Daily, learner and platform counters SHALL remain authoritative in Postgres when Redis or PostHog is unavailable or analytics is disabled.

#### Scenario: Concurrent reservation
- **WHEN** requests race for the same eligible balance or operational capacity
- **THEN** committed holds cannot exceed either balance or configured capacity and repeated acceptance returns the original job

### Requirement: Independent component finalization
Settlement SHALL retain original quote terms, charge delivered token content at its configured minimum up to its ceiling, round aggregate newly synthesized narration characters once, and release failed components. Repeated finalization SHALL return the stored result. Cache/replay components SHALL cost zero. Every provider attempt SHALL retain actual cost separately from customer Credits, including failures. Expired daily allocations released after midnight SHALL expire immediately and never fund tomorrow.

#### Scenario: Price flip and failure
- **WHEN** prices change during a reserved job and only content is delivered
- **THEN** original-version content settles once, audio is released, and actual attempted provider cost is retained

### Requirement: Financial recovery and privacy
Jobs, allocations, usage, counters and the financial outbox SHALL enter encrypted financial backups. Retained ledger and financial history SHALL remain append-only for seven years with account ownership pseudonymized on deletion and exported on request. Restore SHALL preserve exact allocation, balance and cap state. Credits reserved/settled/released/insufficient/exhausted and AI generation events SHALL use the existing allowlisted analytics wrapper and a transactionally deduplicated outbox, respect opt-out and include no learner text.

#### Scenario: Analytics outage
- **WHEN** PostHog is down or the learner opted out
- **THEN** financial finalization and operational limits still work and no private content is emitted

### Requirement: Offline evaluation gate
Recorded mock golden cases SHALL exercise usage/cache envelopes, cap/timeout/fallback behavior and price pinning without provider calls. Live HF/model promotion SHALL remain gated by E10-01 provisioning and E10-06 evaluation.

#### Scenario: Undelivered terminal failure
- **WHEN** bounded attempts fail
- **THEN** the engine returns attempt usage, the API releases customer Credits, and operational spend remains recorded

### Requirement: Safe priced recovery
The client SHALL show catalogue-owned packs, benefits, full billed yearly totals, monthly grants, rollover, taxes, renewal terms, daily eligibility and the next server-confirmed local reset. No recurring offer SHALL be preselected. Insufficient actions SHALL retain mounted input and raw notes. Resume SHALL restore review without sending or charging.

#### Scenario: Insufficient action return
- **WHEN** an action lacks eligible Credits and the learner returns from offers
- **THEN** its saved work and review remain available without automatic reservation or generation

### Requirement: Illustrative checkout isolation
The API SHALL bind pending illustrative orders to the authenticated learner and immutable configured terms, validate quote ownership and fixed internal return targets, and reject stale pricing or unsupported native channels. Illustrative orders SHALL never grant Credits or become verified. Real billing remains disabled until separately integrated and verified.

#### Scenario: Forged successful callback
- **WHEN** an illustrative checkout returns with success parameters or an unrelated wallet increase
- **THEN** status remains pending and no Credits are granted

### Requirement: Recovery financial backup and privacy
Pending orders SHALL enter encrypted financial backups with seven-year live retention and 35-day snapshot rotation. Account export SHALL include owned orders; deletion and restore tombstones SHALL sever personal identity while preserving pseudonymous financial history. Local recovery SHALL contain only disposable identifiers.

#### Scenario: Restore pending order
- **WHEN** financial state is restored
- **THEN** original order terms and pending status survive and erased learners remain unlinked

### Requirement: Recovery analytics privacy
The client SHALL emit paywall_viewed, credits_upsell_shown, pricing_version_exposed, ui_purchase_option_selected and ui_checkout_requested through the existing UUID/consent wrapper. Only fixed identifiers, configured amounts and balances SHALL be included; no learner content SHALL be captured. AI behavior and reliability gates SHALL remain unchanged.

#### Scenario: Opted out learner
- **WHEN** an opted out learner reviews offers and requests illustrative checkout
- **THEN** the flow works without any PostHog delivery

### Requirement: Signed test-mode pack fulfillment
The API SHALL create test-mode provider Sessions only through an explicitly injected provider, using configured pack terms and synthetic price mappings. It SHALL reject live mode until E7-05 enables a separately reviewed integration. Raw-body v1 HMAC-SHA256 and a five-minute timestamp window SHALL precede durable receipt. Canonical paid Session state and matching order, learner pseudonym, environment, SKU, price, amount and currency SHALL precede an atomic purchase, event outcome and financial outbox. Event ID and environment-scoped PaymentIntent SHALL both be unique. Inbox work SHALL survive provider failure and process restart. Late unpaid events SHALL NOT undo a purchase.

#### Scenario: Delayed and reordered payment
- **WHEN** unpaid completion, asynchronous payment, repeated and reordered notifications arrive
- **THEN** pending state grants nothing and canonical paid state appends exactly one purchase and one credits_purchased outbox event

#### Scenario: Invalid evidence
- **WHEN** a signature, timestamp, canonical binding or mode fails validation
- **THEN** no purchase is appended and browser success flags cannot change server state

### Requirement: Payment lifecycle privacy and recovery
Payment bindings, inbox references and outcomes SHALL remain append-only for seven years, with encrypted 35-day financial snapshots. Separately stored allowlisted redacted envelopes SHALL expire after 30 days. Exports SHALL include linked payment records; deletion and restored tombstones SHALL sever learner identity through the original order while retaining dedupe keys. UI SHALL show canonical pending, failed, expired and verified states with explicit refresh and return-to-work actions at desktop/mobile widths, with keyboard, error, dark and reduced-motion parity. Financial analytics checkout_started, credits_purchased and credits_purchase_failed SHALL use the canonical catalogue, unique outbox, common properties and opt-out without learner text. No AI behavior SHALL change.

#### Scenario: Restored payment replay
- **WHEN** financial snapshots are restored and signed events replayed after account erasure
- **THEN** references and balances remain intact without a duplicate grant, personal identity or analytics delivery

### Requirement: Aggregate narration settlement
A Lesson's narration SHALL be reserved with one `narration` quote covering its newly synthesized characters and settled once with only delivered characters summed and rounded up once at the configured rate. Failed audio or alignment MUST be uncharged, the text Lesson MUST stay usable and a retry of the same hash MUST NOT charge a second delivered-artifact price.

#### Scenario: One failed slide
- **WHEN** two slides are delivered and one fails alignment
- **THEN** the reservation settles for the two delivered slides' summed characters rounded up once and the failed slide costs nothing

#### Scenario: Retry
- **WHEN** the failed slide is retried and delivered
- **THEN** only its own characters are charged, once

### Requirement: Learner-paid generation in a Course copy
Reservation, settlement and release for a Lesson generated in a Course copy SHALL belong to the generating Learner at the quoted Pricing config version, covering the content and narration components. An insufficient balance MUST be refused before any Engine call and an undelivered component MUST be released.

#### Scenario: Settle on delivery
- **WHEN** the Engine delivers the Lesson
- **THEN** the Learner's reservation settles and no other account's ledger changes

### Requirement: Source-attributed financial reversals and debt
The API SHALL append compensating ledger entries for partial refunds, full refunds and chargebacks using unique provider reversal keys and referencing the original payment. Reversals MUST reduce only unspent credits from the attributable source payment entry. Cumulative monetary reversals for an order SHALL NOT exceed the original refundable amount. If attributable unspent credits are insufficient to cover a forced reversal or chargeback, the deficit SHALL be recorded as non-spendable source debt. Active source debt SHALL block new reservations requiring paid credits until resolved, while preserving unrelated paid balances and daily free credits.

#### Scenario: Partial refund deduplication and cap
- **WHEN** multiple partial refund notifications describe the same reversal key or exceed the original refundable amount
- **THEN** the reversal key is deduplicated once and excess cumulative reversals are rejected

#### Scenario: Two partial refunds of a partly spent purchase
- **WHEN** a purchase is partly spent and the provider reports two partial refunds, each with its own id
- **THEN** each refund is applied once under its own reversal key, credits are computed from cumulative refunded money, and a refund processed after fulfilment still reverses the purchase exactly once

#### Scenario: Unattributable or malformed refund
- **WHEN** a refund lacks an id, amount or matching currency, or its payment has no fulfilled source entry
- **THEN** no ledger change is made, no other order is reversed and the event is retried

#### Scenario: Chargeback beyond unspent source
- **WHEN** a chargeback is confirmed for a payment source whose credits have already been spent
- **THEN** prior spends are preserved, attributable unspent credits are reduced to zero, non-spendable source debt is recorded and paid AI reservations are blocked

### Requirement: Subscription reversal and grant cancellation
A provider-confirmed refund or chargeback on a subscription invoice SHALL revoke funded entitlements and cancel all unexecuted future grants in the subscription schedule. Already issued credits for the revoked period SHALL be reversed up to the unspent source balance, with any deficit recorded as source debt.

#### Scenario: Annual plan chargeback after consumption
- **WHEN** an annual subscription invoice is charged back after several months of allowance consumption
- **THEN** future monthly grants are cancelled, unspent credits in the source bucket are reversed, any deficit is recorded as debt and active entitlements are revoked

### Requirement: Operator goodwill debt adjustment
Authorized operators SHALL be permitted to waive non-spendable source debt through an auditable adjustment path without manufacturing new spendable credits. Each adjustment MUST record the operator identity, reason code, private notes, timestamp and affected debt records in a database audit table only, MUST fail when no active debt matches, and MUST NOT put the identity or free text in analytics events (reason code only).

#### Scenario: Goodwill debt waiver
- **WHEN** an authorized operator submits an audited goodwill waiver for recorded source debt
- **THEN** the debt status is marked waived and paid AI reservations are unblocked

### Requirement: Bounded financial reconciliation and recovery
The API SHALL durably bound inbox processing to five attempts with backoff and
dead-letter replay. Daily reconciliation MUST retrieve canonical injected TEST
provider state, verify funds and repair missing payments, funded period grants and
provider refund/dispute objects using existing business keys. Unresolved differences
MUST raise an independent operational alert and payment_reconciliation_failed.
Live provider activation SHALL remain rejected under ADR 0012.

#### Scenario: Missing and duplicate notifications
- **WHEN** a verified payment or refund notification was lost and canonical provider objects are reconciled twice
- **THEN** the missing operation is applied once by business key and the second reconciliation adds no financial rows

#### Scenario: Poison receipt
- **WHEN** five processing attempts fail
- **THEN** the durable receipt becomes a dead letter, raises an alert and can be explicitly replayed in a new bounded cycle

### Requirement: Financial retention maintenance
An independent maintenance job SHALL purge redacted provider payloads at 30 days
and derived analytics properties at 90 days. It MUST preserve financial audit keys,
ledger, catalogue, provenance and funded schedules for at least seven years and
reuse the existing encrypted 35-day financial snapshot class.

#### Scenario: Maintenance after restore
- **WHEN** maintenance runs on old financial rows and expired payloads
- **THEN** only short-lived diagnostic and derived properties are purged and retained history can still explain every operation

### Requirement: Credit maintenance timing
Background credit maintenance (abandoned-hold recovery, payment inbox work, outbox delivery, daily expiry) SHALL run only in the worker process, never in API replicas. When idle it SHALL poll no more often than every 5 seconds and back off toward 30 seconds. It MUST read work through indexed due predicates and MUST NOT hold the global financial lock across a scan of many learners: expiry takes the lock once per learner, for that learner's own rows. Ledger semantics are unchanged: reservations, settlement, daily grants (including the lazy grant on a learner's first request of the local day), expiry and annual allowances behave as before.

#### Scenario: Two time zones cross local midnight
- **WHEN** one learner is in Pacific/Auckland and another in America/Los_Angeles, both holding an unspent daily grant for the same UTC instant
- **THEN** each learner's grant expires once, after that learner's own local midnight, and never twice

#### Scenario: Two workers sweep together
- **WHEN** two worker processes run expiry or outbox delivery at the same moment
- **THEN** each expiry row is written once and each outbox event is delivered once

#### Scenario: Wallet read
- **WHEN** a learner reads their wallet
- **THEN** only that learner's past-day grants are expired, not every learner's

#### Scenario: Idle system
- **WHEN** no work is due
- **THEN** an empty outbox costs one indexed probe and no transaction, and one worker plus one API replica send at most 150 statements a minute

### Requirement: Automatic attendance check-in Credits
The first authenticated app open of a new eligible local calendar day MUST call the idempotent check-in endpoint and grant 150 Credits plus a one-time 250 first-check-in bonus (400 on day one). Attendance is not Lesson effort. Amounts and seven bank days MUST come from approved versioned Pricing config. Check-in sources MUST fund every action purchased Credits fund, after eligible Daily free credits and before oldest eligible paid sources. The existing 46-Credit daily allowance, eligibility and expiry MUST remain unchanged.

#### Scenario: First open and retry
- **WHEN** a new Learner opens the app and two devices retry check-in concurrently
- **THEN** exactly one attendance grants 400 Credits, one lifetime bonus exists, and only the winning response triggers a non-blocking localised status confirmation with amount and the spendable balance, which includes the Daily free credits

#### Scenario: App remains open or reconnects
- **WHEN** the returned day boundary arrives or connectivity returns
- **THEN** the client retries check-in without blocking Learn and without granting missed offline days

### Requirement: Atomic check-in bank cap
The regular grant MUST equal min(configured daily grant, max(0, daily grant times bank days minus unspent check-in Credits including reservations)). The first-time bonus MUST then be granted in full once and count toward all later cap calculations. Releases and refunds MUST preserve their source and MUST NOT destroy Credits even if the bank subsequently exceeds the cap. Check-in Credits MUST NOT expire.

#### Scenario: Cap boundary
- **WHEN** an existing Learner checks in with 1049 banked check-in Credits
- **THEN** exactly 1 Credit is granted; at 1050 a new attendance grants zero and the client confirms the bank is full

#### Scenario: Bonus exception
- **WHEN** a first check-in has only 1 Credit of regular cap room
- **THEN** it grants 1 regular Credit plus the full 250 bonus and subsequent regular grants remain zero while the bank exceeds 1050

### Requirement: Durable local-day attendance
Attendance MUST have a unique Learner/local-date key and transactionally enforce the cap and lifetime bonus across replicas. A successful attendance MUST freeze the next midnight in the effective profile time zone. A time-zone change MUST NOT advance that boundary. After the boundary the effective zone determines the date; already attended dates MUST remain deduplicated. Ordinary 23-hour and 25-hour DST days MUST remain eligible at local midnight without a fixed 24-hour cooldown.

#### Scenario: Time-zone change and DST
- **WHEN** a Learner changes zone during an attended day or crosses a DST midnight
- **THEN** a zone change cannot grant within the frozen window, and the next unclaimed eligible local date grants at its midnight

### Requirement: Check-in privacy and backup
Attendance and eligibility state MUST be in Postgres backups, export and erasure coverage and the restore anonymizer registry. Financial ledger rows MUST retain existing pseudonymous retention. Account deletion and re-registration MAY receive a new account's bonus; no identity tracking SHALL be added.

#### Scenario: Restore and deletion
- **WHEN** an account is exported, deleted or restored
- **THEN** attendance is exported, removed on deletion and dedupe/bonus financial history is consistent after restore with deletion tombstones applied

#### Scenario: A full expiry pass fails
- **WHEN** the full-ledger query or any learner expiry transaction fails
- **THEN** the worker does not record the full pass as complete and the next pass retries the full ledger
### Requirement: Daily free spend cap by allocation
The free-learner USD daily cap SHALL count the Daily free allocation of a hold or settlement and SHALL exclude the check-in allocation. Mixed funding SHALL count only its Daily free proportion.

#### Scenario: Check-in funds an outline replan
- **WHEN** a Learner has enough check-in Credits for a replan whose hold exceeds the free daily USD cap
- **THEN** the cap does not reject the check-in-funded hold
