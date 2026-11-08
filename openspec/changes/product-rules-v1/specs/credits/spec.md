## MODIFIED Requirements

### Requirement: Append-only ledger
Credits SHALL be stored as entries in `credit_entries` with learner, amount, kind (`daily_grant`, `purchase`, `subscription_grant`, `spend`, `reserve`, `release`, `refund`, `expire`) and reference. Entries MUST NOT be updated or deleted. Balance MUST be derived from entries and MAY be cached. Daily free credits, purchased Credits and subscription Credits MUST be separable by kind. There MUST be no `reward` kind.

#### Scenario: Correction
- **WHEN** an operator corrects a wrong charge
- **THEN** a new `refund` entry is appended and the old entry is untouched

#### Scenario: Cache lost
- **WHEN** the balance cache is empty
- **THEN** the balance is recomputed from entries

### Requirement: Insufficient balance
When the spendable balance is lower than the estimate, the API SHALL refuse the AI request with `insufficient_credits`, MUST NOT call the Engine, and MUST return the Learner's next Daily free credits grant time and the available top-up options. The client MUST show an upsell screen with those options. No AI feature MAY fail silently for lack of Credits. Spendable balance MUST exclude expired Daily free credits.

#### Scenario: Low balance
- **WHEN** a Learner with zero spendable Credits opens Tutor chat and sends a message
- **THEN** the request is refused and the upsell screen is shown

### Requirement: Credits analytics events
The system SHALL emit `credits_reserved`, `credits_settled`, `credits_released`, `credits_insufficient`, `credits_purchased`, `credits_purchase_failed`, `daily_credits_granted`, `daily_credits_expired` and `credits_upsell_shown` with properties from the `analytics` spec.

#### Scenario: Settled
- **WHEN** a reservation is settled
- **THEN** `credits_settled` is emitted with `feature`, `amount`, `model` and `input_tokens` and `output_tokens`

#### Scenario: Daily grant
- **WHEN** the daily grant is written for a Learner
- **THEN** `daily_credits_granted` is emitted once for that local day with `amount`

## ADDED Requirements

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

## REMOVED Requirements

### Requirement: Reward credits
**Reason**: Owner decision (ADR 0007): effort is never rewarded with Credits.
**Migration**: Drop the `reward` kind in the E12 migration; reward events and config keys are deleted. Existing `reward` entries, if any, stay readable in history and are not spent.
