## ADDED Requirements

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
