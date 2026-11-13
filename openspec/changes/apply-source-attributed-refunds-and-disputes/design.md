# Design: Apply source-attributed refunds and disputes

## Context & Principles
Compensating financial operations must preserve the append-only ledger invariant. Neither past purchases nor past spends are ever deleted or edited. Reversals affect only the attributable source and any future unearned benefits.

1. **Reversal Identification & Deduping**:
   Reversals are identified by a unique `reversal_key` formatted as `(provider, environment, refund_or_dispute_id, operation)` (e.g. `stripe:test:re_123:refund` or `stripe:test:dp_123:chargeback`).
   Duplicate notifications for the same reversal key are idempotent no-ops.

2. **Monetary Cap**:
   Cumulative refunded amount (in minor units) for an order cannot exceed the original `credit_checkout_orders.amount_minor`. Notifications exceeding this cap are rejected.

3. **Source Attribution & Unspent Calculation**:
   - For pack purchases, the original grant row `credit_entries` (`kind='purchase'`) is the source.
   - For subscriptions, the period grant row `credit_entries` (`kind='subscription_grant'`) is the source.
   - Unspent credits on a source are calculated from `source.amount + SUM(entry.amount)` where `entry.provenance->>'source_id' = source.id::text`.
   - Proportional credits to reverse for partial refunds: `(order.credits * refund_amount_minor) / order.amount_minor`.
   - Credits to deduct from ledger: `min(credits_to_reverse, unspent_source_credits)`.
   - Compensating ledger entry: `kind='refund'`, negative amount `-credits_to_deduct`, with reference `reversal_key`.
   - Deficit: `credits_to_reverse - credits_to_deduct`.

4. **Source Debt & AI Reservation Blocking**:
   - When `deficit > 0`, a row is recorded in `credit_debt_history` with status `active` and amount `deficit`.
   - As long as active debt exists for the learner, `Reserve` rejects any reservation that requires paid credits (`ErrSourceDebt`).
   - Daily free credits remain spendable on daily-free eligible actions (notes, short tutor chat) up to the daily allowance.
   - Unrelated paid credits and daily balances are preserved and never seized to pay down debt automatically.

5. **Subscription Revocation**:
   - Provider-confirmed refund or chargeback on a subscription invoice revokes active entitlements (`credit_subscription_revocations`), cancels all remaining scheduled future grants in `credit_subscription_schedule`, and emits `entitlement_changed`.

6. **Audited Goodwill Adjustment**:
   - An authorized operator may waive source debt (`ApplyGoodwillAdjustment`). The adjustment updates the debt row status to `waived_goodwill`, records operator ID and audit reason, and logs an audited event without creating new spendable credits.

7. **Data Retention & Privacy**:
   - New tables `credit_reversals`, `credit_refund_allocations`, `credit_dispute_keys`, `credit_debt_history` are retained 7 years in financial backups.
   - On account deletion, `learner_id` is set to NULL while `owner_reference` is pseudonymized (SHA-256 hash).
   - Restore drill anonymizer classifies columns as safe identifiers/metadata.
