## ADDED Requirements

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
The system SHALL emit `pricing_version_exposed` (pricing_version, surface: `paywall`, `quote`, `pack_list`, `plan_list`) at most once per session, surface and version, and `pricing_version_created`, `pricing_version_approved`, `pricing_version_rejected`, `pricing_version_activated` and `pricing_version_rolled_back` (pricing_version, change_class: `adverse` or `non_adverse`, actor_id) from the API. Events MUST NOT contain price secrets, Stripe keys or reason text. Existing monetization events use the same version id as `price_table_version`.

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
