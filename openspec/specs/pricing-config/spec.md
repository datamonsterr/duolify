# pricing-config Specification

## Purpose
Keep every Credit price, pack, Subscription tier and cost limit in one versioned, audited Pricing config in Postgres. Prices change without a code deploy, and each ledger entry and quote records the price version it used.
## Requirements
### Requirement: Single versioned Pricing config
Every credit price of an AI action, the Daily free credits amount, every credit pack, every Subscription tier with its benefits and limits, and the generation and cost limits SHALL live in one versioned Pricing config stored in Postgres. No price, credit amount, pack, tier or benefit value MAY be hard-coded in the client, the API logic or the Engine. Versions MUST be immutable once approved.

#### Scenario: Price lookup
- **WHEN** any component needs the credit cost of an action
- **THEN** it reads the active Pricing config version and no constant in source code

#### Scenario: Edit approved version
- **WHEN** someone tries to modify an approved version
- **THEN** the change is refused and a new draft version must be created

### Requirement: Seed values are the approved E12 table
Version 1 SHALL equal the owner-approved E12 table in `docs/business/monetization.md`: preview 25, outline 55, lesson content 140, narration 50 Credits per 1,440 newly synthesized characters, tutor short 5, tutor long 8, quick note 2, cached replay 0; Daily free credits 46; packs USD 5/400, USD 15/1,350, USD 40/4,000 Credits; Learn USD 9.99 monthly or USD 96 yearly with 600 Credits per service month; Plus USD 19.99 or USD 192 with 1,600; Pro USD 39.99 or USD 384 with 3,600; their benefits and limits as written there. A seed migration MUST load it and a test MUST compare the loaded values to the approved table.

#### Scenario: Fresh database
- **WHEN** migrations run on an empty database
- **THEN** version `usd-launch-2026-11-08` is active with exactly the approved values

#### Scenario: Value drift
- **WHEN** the seed file differs from the approved table
- **THEN** the seed test fails

### Requirement: Schema validation at load
Every version MUST be validated against a JSON Schema and cross-field rules (integer non-negative Credits, known action keys, unique SKUs, consistent currency, rational narration rate, annual plan present for each tier, daily free eligible actions are known actions) when drafted, when submitted and when loaded by a running API. An invalid version MUST NOT become active. If no valid active version exists at boot the API MUST refuse to start; if a reload fails at runtime the API MUST keep the last known good version and alert.

#### Scenario: Invalid draft
- **WHEN** a draft has a negative Credit amount
- **THEN** `validate` fails with the field path and the draft cannot be submitted

#### Scenario: Reload failure
- **WHEN** the stored active version fails validation during a reload
- **THEN** the API keeps serving the last known good version and an alert fires

### Requirement: Change without a code deploy
Authorized operators SHALL create, validate, diff, submit, approve, schedule, cancel and roll back versions through the admin CLI (`admin pricing ...` on the API binary) or `/admin/v1/pricing/*` endpoints, without a code deploy. Access MUST require the `pricing_admin` role. Every operation MUST require a reason and write an audit entry.

#### Scenario: Price change live
- **WHEN** an approved version reaches its `effective_from`
- **THEN** new quotes use it and no deployment happened

#### Scenario: Non-admin
- **WHEN** a Learner or a user without `pricing_admin` calls an admin pricing endpoint
- **THEN** the API responds 403 and writes a denied-access audit entry

### Requirement: Adverse changes need two-person approval
The API MUST compute from the diff whether a version is adverse to Learners: a higher action cost, a lower Daily free credits amount, a higher pack or plan price, fewer Credits per pack or per service month, a removed entitlement or a tightened limit. An adverse version MUST be approved by a `pricing_admin` other than its author and MUST have `effective_from` at least the configured notice after approval. A non-adverse version SHALL need one `pricing_admin` approval, which MAY be the author's.

#### Scenario: Self-approval of increase
- **WHEN** the author of a draft that raises the Build pack price tries to approve it
- **THEN** approval is refused until a second `pricing_admin` approves

#### Scenario: Notice too short
- **WHEN** an adverse version is scheduled sooner than the notice window
- **THEN** scheduling is refused

#### Scenario: Mixed change
- **WHEN** a version lowers one price and raises another
- **THEN** it is classified adverse

### Requirement: Effective-from scheduling
Each version SHALL have an `effective_from` timestamp compared against database time. The resolver MUST return the approved version with the greatest `effective_from` not after the instant asked, so exactly one version is active at any instant. A scheduled version MAY be cancelled before it takes effect. Quotes and reservations created before the flip MUST keep their original version.

#### Scenario: Flip during generation
- **WHEN** a version becomes active while a generation job holds a reservation
- **THEN** the job settles at the reservation's version and cannot exceed its approved ceiling

#### Scenario: Two versions same instant
- **WHEN** two approved versions share an `effective_from`
- **THEN** scheduling the second is refused

### Requirement: Audit log
Every create, edit, validation failure, submission, approval, rejection, schedule, cancel, activation, rollback and denied access MUST append an entry with actor, action, version, diff summary, reason, request id and time. Audit entries MUST NOT be updated or deleted.

#### Scenario: Who raised the price
- **WHEN** an auditor asks who changed the Plus monthly price
- **THEN** the log shows author, approver, reason, diff and times

#### Scenario: Tamper attempt
- **WHEN** an UPDATE or DELETE is run on an audit row
- **THEN** the database rejects it

### Requirement: Price version on ledger entries and quotes
Every quote, reservation, `credit_entries` row, order and subscription grant MUST store the Pricing config version id it used. Past rows MUST NOT be rewritten when a new version activates.

#### Scenario: Refund after a price change
- **WHEN** a purchase made at version A is refunded after version B is active
- **THEN** the refund references version A and its original amounts

### Requirement: Clients fetch current prices from the API
The API SHALL expose `GET /v1/pricing` returning the active version, its id, `effective_from` and the next scheduled version when one is approved. Clients MUST render prices, pack sizes, plan benefits and action costs from this response or from API quotes, MUST treat the server quote as authoritative, and MAY cache the last response for offline display labelled with its version. A lint rule or test MUST fail the client build on price or Credit literals outside test fixtures.

#### Scenario: Price change without app update
- **WHEN** a new version becomes active
- **THEN** an installed client shows the new prices on its next fetch with no app update

#### Scenario: Stale offline price
- **WHEN** the client is offline and shows cached prices
- **THEN** it labels them as last known and the API quote decides the charge when online

### Requirement: Stripe price mapping
The Pricing config SHALL map each pack and plan SKU to Stripe price IDs per environment (test, live) and to store product IDs. Before activation the API MUST verify each Stripe price (exists, active, amount, currency, recurring interval) and MUST refuse activation on mismatch. Stripe price IDs of retired versions MUST be retained. Config MUST NOT contain secret keys.

#### Scenario: Amount mismatch
- **WHEN** the config says USD 19.99 and the Stripe price is USD 18.99
- **THEN** activation is blocked with the SKU and difference

#### Scenario: Price increase for a SKU
- **WHEN** a plan price changes
- **THEN** the new version references a new Stripe price ID and existing subscribers keep their original ID

### Requirement: Rollback
An operator SHALL be able to roll back by creating a new version whose content equals a chosen earlier version. Rollback MUST follow the same approval rules as any version, judged against the currently active version.

#### Scenario: Rollback to lower price
- **WHEN** an operator rolls back to an earlier lower-priced version
- **THEN** a new version is created, non-adverse, and activates after one approval

#### Scenario: Rollback to higher price
- **WHEN** the earlier version has a higher price than the active one
- **THEN** it is adverse and needs a second approver and notice

### Requirement: Cost guardrail validation
Validation MUST check that every priced standard action keeps its protected cost per Credit at or below the configured `protected_cost_ceiling_usd` using the current provider and model prices, and MUST report the result in `validate`. A failing check MUST block submission unless an operator records an explicit override reason, which is audited.

#### Scenario: Unprofitable price
- **WHEN** a draft sets lesson content to 60 Credits and the check exceeds the ceiling
- **THEN** `validate` fails and names the action and computed cost per Credit

### Requirement: Pricing analytics events
The system SHALL emit `pricing_version_exposed` (pricing_version, surface: `paywall`, `quote`, `pack_list`, `plan_list`) at most once per session, surface and version, and `pricing_version_created`, `pricing_version_approved`, `pricing_version_rejected`, `pricing_version_activated` and `pricing_version_rolled_back` (pricing_version, change_class: `adverse` or `non_adverse`) from the API. Events MUST NOT contain price secrets, Stripe keys or reason text. Existing monetization events use the same version id as `price_table_version`.

#### Scenario: Paywall shown
- **WHEN** a client shows the paywall twice in one session
- **THEN** one `pricing_version_exposed` with surface `paywall` is emitted

#### Scenario: Activation
- **WHEN** a scheduled version becomes active
- **THEN** `pricing_version_activated` is emitted once

### Requirement: Pricing data backup and retention
Pricing versions, the audit log and Stripe price mappings MUST be stored in Postgres and covered by the nightly dump. Versions and audit entries MUST be retained at least 7 years (the financial record retention in `credits`), including after an admin account is deleted (actor pseudonymized). Redis holds only a cache. A restore drill MUST show the same active version resolves after restore.

#### Scenario: Restore
- **WHEN** the database is restored from a dump
- **THEN** the active version at the restore time resolves identically and the audit log is intact

#### Scenario: Admin leaves
- **WHEN** an admin account is deleted
- **THEN** audit entries keep a pseudonymous actor id and remain

### Requirement: Immutable action quotes independent of reservations
Authenticated Learners SHALL create quotes with `POST /v1/quotes`, read original
terms with `GET /v1/quotes/{id}`, and record consent before expiry using
`POST /v1/quotes/{id}/accept`. Quotes MUST snapshot the active database version,
USD currency, separate content/audio ceilings, daily-bucket eligibility, model,
context bracket, input/output bounds, rational narration rate and voice allowlist.
Unknown rates or out-of-envelope inputs MUST fail closed before provider work.
Quote policy SHALL be an immutable version-owned extension of Pricing config.
Development defaults pending owner review are a 600-second TTL, logical voice
`default`, and the business-model provider/model rates; no model is enabled by
quoting it. Money SHALL use integer USD nanodollars and Credits integer arithmetic.

#### Scenario: Consent after a flip
- **WHEN** a Learner accepts an unexpired quote and the active catalogue changes
- **THEN** its original terms remain readable and unchanged without a wallet reservation

#### Scenario: Expired consent
- **WHEN** a Learner attempts to accept an expired quote
- **THEN** the API returns conflict and requires a fresh quote and renewed consent

#### Scenario: Quote retention and privacy
- **WHEN** a Learner deletes their account or an encrypted backup is restored
- **THEN** quotes, consent and version references survive for at least seven years with pseudonymous ownership and without learner text; encrypted snapshots rotate after 35 days per ADR 0011

#### Scenario: Quote exposure
- **WHEN** original authoritative quote terms appear
- **THEN** pricing_version_exposed(surface=quote) uses the quoted version through the existing UUID/opt-out analytics wrapper without private text

#### Scenario: Evaluation gates
- **WHEN** a quote names a provider or voice
- **THEN** no provider call occurs and existing feature evaluation and fallback gates remain required

### Requirement: Database-time activation without deployment
The API SHALL resolve the approved, non-cancelled pricing version with the greatest `effective_from <= database now()` and expose the next future approved, non-cancelled version. Scheduling SHALL use a unique effective instant, require a future instant, and preserve the existing adverse notice and approval rules. Cancellation SHALL be allowed only before the scheduled instant and SHALL retain the immutable version and audit history. Runtime reload failure SHALL retain the last known good validated catalogue and log an alert. Redis loss SHALL leave Postgres resolution available.

#### Scenario: Concurrent scheduled flip
- **WHEN** database time reaches a scheduled version's `effective_from` during concurrent reads
- **THEN** all reads resolve the same greatest eligible version and the next scheduled value is consistent

#### Scenario: Cancel a pending schedule
- **WHEN** an administrator cancels a schedule before its effective instant
- **THEN** it no longer resolves or appears as next scheduled, while its immutable version and cancellation audit remain

#### Scenario: Reload failure
- **WHEN** runtime loading encounters invalid stored data or a database error
- **THEN** the API serves the last validated catalogue and records an operational alert

### Requirement: Immutable rollback ancestry and once-only activation
Rollback SHALL create a new draft from the selected historical content, record both the currently active parent and the historical rollback source, and use normal submission, adverse classification, notice and approval. Existing quotes, grants, reservations and paid periods SHALL retain their original version. Activation SHALL be recorded once per version and emit `pricing_version_activated` and `price_table_activated` with common UUID properties and no private content.

#### Scenario: Roll back to a higher price
- **WHEN** a rollback raises a learner-facing price relative to the active version
- **THEN** it is classified adverse and cannot become effective until approved under the normal adverse workflow

#### Scenario: Previously accepted quote
- **WHEN** a version activates after a quote was accepted
- **THEN** the quote retains its original version and accepted terms

#### Scenario: Once-only activation
- **WHEN** multiple API instances observe the same activation
- **THEN** a durable unique marker permits only one activation event emission per version

### Requirement: Development setup chat pricing key
The versioned Pricing config SHALL support the `setup_chat` action key with proposed platform-paid credit cost of 0 in development configuration explicitly marked unapproved (`approved: false`). No Learner Credits SHALL be deducted for `setup_chat` in this free mode. Production activation of `setup_chat` SHALL require an approved Pricing config version created through E12-10; while disabled in production config, setup MUST retain the accessible form without blocking onboarding.

#### Scenario: Free setup chat quote in development
- **WHEN** a quote is requested for `setup_chat` under the unapproved development pricing configuration
- **THEN** the returned quote specifies 0 Credits ceiling with platform-paid status and zero Learner charge

#### Scenario: Production disabled fallback
- **WHEN** the active approved pricing configuration lacks the `setup_chat` action key
- **THEN** the API signals that setup chat is unavailable and the client falls back to the form

### Requirement: Setup chat pricing analytics
Pricing quote operations and free setup chat requests SHALL NOT emit paid credit deduction events. Interactions with setup chat pricing quotes MUST record only anonymous platform usage metrics through existing `$ai_generation` tracking without private Learner text. Opted-out Learners MUST NOT produce any analytics events.

#### Scenario: Zero-charge analytics privacy
- **WHEN** a Learner completes setup chat at zero cost
- **THEN** no financial deduction or private text is recorded in analytics

### Requirement: Setup chat pricing data backup and retention
Pricing versions containing the unapproved `setup_chat` definition SHALL be stored immutably in Postgres `pricing_versions` and covered by full nightly database backups retained according to the 7-year financial catalogue policy. No transient setup chat state SHALL be written to financial quote or ledger tables.

#### Scenario: Backup of unapproved pricing version
- **WHEN** a database backup is taken
- **THEN** the unapproved development pricing version is preserved with its `approved: false` flag intact

### Requirement: Development course brief chat funding
The Pricing config SHALL define course_brief_chat as platform-paid with zero learner Credits in immutable unapproved development version usd-dev-course-brief-unapproved, succeeding usd-dev-unapproved without mutation. Its numeric envelope SHALL contain token, attempt, deadline, USD and rate ceilings. The API MUST enforce learner and global platform throttles shared across replicas, bounded by the platform USD/day cap. Development MUST remain independent of launch sales approval. Production MUST require an approved Pricing version and explicit live promotion; form confirmation remains usable while chat is disabled. No wallet reservation or deduction SHALL occur during clarification.

#### Scenario: Zero-balance development learner
- **WHEN** a Learner with no Credits clarifies a brief under the development config
- **THEN** the model may be called within platform limits and no ledger entry is created

#### Scenario: Unapproved production config
- **WHEN** production selects the unapproved development version
- **THEN** assistant calls are refused and the form stays available

### Requirement: External payment mapping verification
Before any pricing catalogue version is activated for sale, the API SHALL verify all external payment mappings against provider APIs or test doubles. For Stripe, verification SHALL check that each mapped price exists, is active, matches the configured currency and minor-unit amount, and matches the recurring interval (one-time for packs, monthly count 1 for monthly offers, yearly count 1 for yearly offers) in the target environment. Store-localized product ID mappings SHALL be validated according to channel-specific conventions (Apple, Google). Stripe price IDs from retired versions SHALL be retained immutably in their respective version records.

#### Scenario: Wrong Stripe price amount
- **WHEN** a pricing draft maps a pack or plan to a Stripe price ID with an amount different from the configured catalogue amount
- **THEN** validation or activation is blocked with the mismatching SKU and amounts

#### Scenario: Wrong recurring interval
- **WHEN** a monthly subscription offer is mapped to a yearly or one-time Stripe price
- **THEN** validation or activation is blocked with the interval mismatch

### Requirement: Catalogue economics and margin floor
The API and Engine SHALL verify that every standard action's protected cost per credit does not exceed the configured protected cost ceiling, and that every sellable SKU maintains a paid-AI gross margin of at least 70% under configured fees, refund reserves, and full credit redemption at the ceiling cost. An action exceeding the cost ceiling SHALL block submission unless an audited override reason is explicitly supplied; a SKU falling below 70% margin SHALL always block activation and cannot be overridden. Daily free credits SHALL be sized to match the exact combined envelope of 3 quick notes and 8 short (or 5 long) tutor messages.

#### Scenario: Uneconomic SKU below 70% margin
- **WHEN** a draft sets pack or plan pricing resulting in net margin below 70%
- **THEN** submission and approval are refused regardless of operator role or notice window

#### Scenario: Cost ceiling override
- **WHEN** a standard action exceeds the cost ceiling and an operator provides an explicit override reason
- **THEN** the override is recorded in the audit log and submission is permitted, provided all SKUs meet the 70% margin floor

### Requirement: Verification evidence audit and analytics
Every external mapping check and catalogue economics evaluation SHALL append an immutable audit record to `pricing_audit` with the evaluation evidence, actor hash, and request identifier. Rejections SHALL emit `pricing_version_rejected` to operational telemetry with sanitized properties and without private operator reason text. Verification evidence SHALL be retained with immutable versions for at least seven years.

#### Scenario: Verification failure audited
- **WHEN** validation fails due to invalid provider mapping or uneconomic margin
- **THEN** an audit entry records the failure evidence and `pricing_version_rejected` is emitted with UUID actor and version properties

### Requirement: Configurable payment processing fees and refund reserve
The pricing catalogue configuration schema SHALL permit a `processing_fees` object specifying payment provider fee rates and refund reserves (`pack_fee_rate`, `pack_fixed_fee_usd`, `subscription_fee_rate`, `subscription_fixed_fee_usd`, `refund_reserve_rate`). When omitted in catalogue versions, the engine and API SHALL use the development-proposed default rates (pack: 2.9% + $0.30, subscription: 3.6% + $0.30, refund reserve: 2.0%) flagged as dev-proposed pending final owner review.

#### Scenario: Catalogue with custom processing fees
- **WHEN** a catalogue draft provides `processing_fees`
- **THEN** SKU margin calculations use the configured fee rates and refund reserve rather than hardcoded literals

### Requirement: Design chat pricing keys and interim governance
The versioned Pricing config SHALL support `setup_chat` (0 Credits), `course_brief_chat` (0 Credits), and `outline_chat_change` (5 Credits) with Daily free eligibility for `outline_chat_change`. A version introducing any of these keys SHALL record `design_chat_decision` ("interim_adr_0012" or "owner_approved"). Outside explicit `development` or `test` environments (`DUOLIFY_ENV`), approving, activating, or quoting a version containing these keys SHALL be rejected unless `design_chat_decision` is `owner_approved` AND the version was approved by `PRICING_OWNER_ID`. In `development` and `test`, the interim decision `interim_adr_0012` MAY be approved and activated. Pending or missing decision values SHALL NOT enable these actions in any environment.

#### Scenario: Production rejection of interim decision
- **WHEN** an administrator attempts to approve or activate a version with `design_chat_decision: interim_adr_0012` in production
- **THEN** approval or activation is rejected and the active catalogue remains unchanged

#### Scenario: Production approval with owner decision
- **WHEN** the `PRICING_OWNER_ID` administrator approves a version with `design_chat_decision: owner_approved` in production
- **THEN** approval succeeds and verification evidence is recorded

#### Scenario: Development approval of interim decision
- **WHEN** an administrator approves a version with `design_chat_decision: interim_adr_0012` under `DUOLIFY_ENV=development`
- **THEN** approval succeeds and the version becomes eligible for database-time activation

### Requirement: Action flags and rollback isolation
The actions `setup_chat`, `course_brief_chat`, and `outline_chat_change` SHALL be enabled if and only if the active (activated) pricing version contains the corresponding key and passes the decision gate. When active pricing does not contain the key or when a forward rollback version omitting the key is activated, these actions SHALL be disabled immediately. Manual outline editing, proposal Apply, proposal Discard, and form fallbacks SHALL remain available without error when chat actions are disabled. In-flight reservations created under an earlier active version SHALL settle against their original quoted version.

#### Scenario: Rollback disables chat actions while preserving catalogue
- **WHEN** a forward rollback version without design chat keys is activated
- **THEN** `setup_chat`, `course_brief_chat`, and `outline_chat_change` become disabled while unrelated actions remain active and quotable

#### Scenario: In-flight reservation settlement across rollback
- **WHEN** a proposal reservation is held under an activated version that is subsequently rolled back
- **THEN** validated proposal delivery settles at the original reservation quote version

### Requirement: Validated proposal settlement and free actions
`outline_chat_change` SHALL charge Credits only upon delivery of a valid generated proposal. A failed or timed-out proposal generation SHALL release the held reservation without deducting Credits. Applying a delivered proposal, discarding a proposal, undoing an applied change, and manual outline edits SHALL be free of charge.

#### Scenario: Delivery charges 5 credits
- **WHEN** a valid outline proposal is generated and delivered
- **THEN** exactly 5 Credits are settled against the reservation

#### Scenario: Generation failure releases reservation
- **WHEN** outline proposal generation fails or provider errors occur
- **THEN** the reservation is released and 0 Credits are deducted

### Requirement: Owner-approved design chat catalogue
A new immutable Pricing config version SHALL record `design_chat_decision: owner_approved`, `setup_chat=0`, `course_brief_chat=0` and `outline_chat_change=5` Credits, with Daily free credits eligible for the latter. It SHALL retain the interim configured usage/cost envelopes, `outline_replan=55`, and explicit policy that clarifying chats are platform paid, only a valid delivered outline proposal is charged even if discarded, undelivered proposals release reservations, and manual edits, Undo, Apply and Discard are free. Historical versions MUST remain unchanged.

#### Scenario: Production owner approval
- **WHEN** the version passes ordinary notice, cost and mapping validation and the configured `PRICING_OWNER_ID` administrator approves it
- **THEN** it may be scheduled and activated, and new quotes disclose its original prices and eligibility

#### Scenario: Decision or owner missing
- **WHEN** production approval or activation lacks the recorded owner-approved decision or matching owner reviewer
- **THEN** the design chat version is rejected without blocking unrelated catalogue actions

#### Scenario: Development remains available
- **WHEN** a granted administrator approves an interim ADR 0012 version in development or test
- **THEN** the existing interim path remains available

#### Scenario: Forward rollback and retention
- **WHEN** an approved forward version removes only the three design chat actions
- **THEN** unrelated prices including outline_replan remain unchanged, historical quotes retain their version, and encrypted financial backup/restore preserves the version, policy, reviewer audit and activation history for seven-year retention

#### Scenario: Live gates remain independent
- **WHEN** the fixture is committed or drafted
- **THEN** no live activation, sales, feature flags, provider promotion or model/fallback change occurs; existing canonical pricing analytics remain content free

### Requirement: Bounded-freshness catalogue snapshot
`GET /v1/pricing` SHALL serve an in-memory snapshot of the active catalogue that is at most 5 seconds older than the last successful Postgres read, and SHALL run no SQL for a request inside that bound. A process that activates a version SHALL serve it immediately; another replica SHALL serve it within the bound after the activation marker exists, so a scheduled flip is served everywhere within the activator interval plus the bound. A failed reload SHALL keep serving the last validated catalogue, raise the existing alert and be retried no more than once per bound. The response SHALL carry an ETag and `Cache-Control: public, max-age=10`.

#### Scenario: Warm request
- **WHEN** the snapshot is inside its bound
- **THEN** requests, including conditional ones, run no database statement

#### Scenario: Reload failure
- **WHEN** the database is unavailable after the snapshot expires
- **THEN** the last good catalogue is served, the alert is raised, and the database is queried at most once per bound

### Requirement: Owner-approved Hy4/GLM routing version
An immutable approved Pricing version SHALL quote every AI action on `tencent/hy4-flash` with `z-ai/glm-5.3-flash` as fallback, both through `openrouter`, with credit prices, packs, plans and limits identical to the previous approved snapshot. It SHALL be the active quote version once its effective instant has passed, and the unapproved proposal row SHALL remain unapproved and never active.

#### Scenario: Preview quotes the served route
- **WHEN** a learner requests a preview quote after migrations
- **THEN** the quote names the approved version, `openrouter/tencent/hy4-flash` and its Hy4 rate, and settlement of Hy4 or GLM usage charges that model's own rate while a Claude model is rejected

#### Scenario: Active version matches the engine
- **WHEN** CI compares the active approved version with `engine/config/models.toml` and the startup route check runs with engine providers `openrouter`/`openrouter`
- **THEN** no action is reported as unserved

#### Scenario: Later learner price changes
- **WHEN** a later version changes a learner-facing price
- **THEN** it still goes through draft, owner approval and the 72-hour adverse-notice rule

