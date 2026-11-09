# credits Specification

## Purpose
Charge for AI features with prepaid Credits kept in an append-only ledger. Credits are reserved before an AI call and settled with actual usage. Learners can top up and can earn Reward credits through effort.

## Requirements

### Requirement: Append-only ledger
Credits SHALL be stored as entries in `credit_entries` with learner, amount, kind (`purchase`, `reward`, `spend`, `reserve`, `release`, `refund`) and reference. Entries MUST NOT be updated or deleted. Balance MUST be derived from entries and MAY be cached. Purchased credits and Reward credits MUST be separable by kind.

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
The price of each AI action MUST be computed from per-model prices in config and the reported `Usage`. Price changes MUST NOT alter past entries.

#### Scenario: Price update
- **WHEN** a model price changes
- **THEN** new calls use the new price and old entries stay as written

### Requirement: Insufficient balance
When the balance is lower than the estimate, the API SHALL refuse the AI request with `insufficient_credits`, MUST NOT call the Engine, and the client MUST offer a top-up.

#### Scenario: Low balance
- **WHEN** a Learner with zero Credits opens Tutor chat and sends a message
- **THEN** the request is refused and a top-up prompt is shown

### Requirement: Top-ups
Learners SHALL add Credits through Stripe Checkout opened from the app. A purchase entry MUST be written only after a verified Stripe webhook, and webhook handling MUST be idempotent by event id.

#### Scenario: Webhook replay
- **WHEN** Stripe sends the same `checkout.session.completed` event twice
- **THEN** one `purchase` entry exists

### Requirement: Reward credits
The system SHALL grant Reward credits (kind `reward`) for effort: Streak milestones, perfect Lesson tests and Friend streak milestones. Rewards MUST be capped per day by config to limit abuse, and each reward MUST be granted once per trigger.

#### Scenario: Streak milestone
- **WHEN** a Streak reaches a milestone
- **THEN** a `reward` entry is appended once and `reward_credits_granted` is emitted

#### Scenario: Spend order
- **WHEN** a Learner has both kinds
- **THEN** Reward credits are spent before purchased Credits unless config says otherwise

### Requirement: Credits analytics events
The system SHALL emit `credits_reserved`, `credits_settled`, `credits_released`, `credits_insufficient`, `credits_purchased`, `credits_purchase_failed` and `reward_credits_granted` with properties from the `analytics` spec.

#### Scenario: Settled
- **WHEN** a reservation is settled
- **THEN** `credits_settled` is emitted with `feature`, `amount`, `model` and `input_tokens` and `output_tokens`

### Requirement: Credits data backup and retention
The ledger MUST be stored in Postgres and covered by the nightly dump and by point-in-time recovery where available. Ledger entries MUST be kept at least 7 years, or as the law for payment records requires. After a restore, balances MUST match the sum of entries.

#### Scenario: Restore drill
- **WHEN** the restore drill finishes
- **THEN** a check query confirms derived balance equals the sum of restored entries for every Learner

### Requirement: Cost reliability
The API MUST enforce a per-request, per-day and per-learner spend cap from config, and MUST alert when platform AI cost per active Learner exceeds a budget. Estimates MUST be tuned using Golden set token statistics.

#### Scenario: Runaway reply
- **WHEN** a streamed reply reaches the max output tokens
- **THEN** the stream stops and settlement uses the actual usage up to the cap
