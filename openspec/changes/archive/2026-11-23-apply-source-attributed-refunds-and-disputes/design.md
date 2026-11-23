# Design: Apply source-attributed refunds and disputes

## Context & Principles
Compensating financial operations must preserve the append-only ledger invariant. Neither past purchases nor past spends are ever deleted or edited. Reversals affect only the attributable source and any future unearned benefits.

1. **Provider objects and reversal identity**:
   The injected signed test provider's canonical charge (and subscription invoice) state carries lists of refunds `{id, amount_minor, currency, status}` and disputes `{id, amount_minor, currency, status}`. Stripe's `amount_refunded` is cumulative per charge; each refund is its own object. Processing any verified notification re-reads canonical state and reconciles every object not yet applied. The reversal key is `provider:environment:object id` (e.g. `stripe:test:re_123`). Ids and amounts are never defaulted; missing/invalid ids, amounts, currency mismatch, duplicate ids, unknown statuses and an unfulfilled or unattributable source fail closed: no ledger change, the inbox event stays unprocessed for retry/dead-letter. Applied statuses: refund `succeeded`; dispute `needs_response`, `under_review`, `lost`. `pending`/`failed` refunds and `won`/`charge_refunded`/`warning_*` disputes are skipped. Reversals only enter through the verified signed notification path (no synthetic inbox rows).

2. **Ordering**: fulfilment first, then reversals, in the same transaction. A completion processed after (or in the same reconcile as) a refund still fulfils exactly once and is then reversed.

3. **Cap and cumulative arithmetic**: cumulative reversed money (refunds + disputes) per payment (pack order, or subscription term/invoice) cannot exceed the payment amount. Credits are `floor(source_credits × total_reversed_minor / amount_minor)` (integer arithmetic; full amount = exactly the source credits); a reversal removes the difference from credits already reversed, so rounding never loses or over-reverses credits.

4. **Source attribution**: the source is only this payment's own ledger entries: the `purchase` entry of the order's paid result, or the term's own grants (month 0 plus issued annual schedule grants). No source → fail closed. No fallback to another order. Allocation walks sources newest first: per source `deduct = min(attributable, unspent)`, remainder is non-spendable debt attributed to that source grant (`credit_refund_allocations`, `credit_debt_history`). Unspent = grant + all ledger entries carrying that `source_id` (spends, earlier reversals). Compensating `refund` ledger entries reference the reversal key; history is never edited.

5. **Subscriptions and annual terms**: any confirmed reversal on a term inserts the term's revocation once (entitlement off; `IssueAnnualAllowances` skips revoked terms, cancelling every future grant) and emits `entitlement_changed` once; issued grants are reversed as in (4) with prior spends preserved. Every error is propagated.

6. **Debt gate**: active debt blocks new reservations that need paid (purchase/subscription) credits (`ErrSourceDebt`); daily free credits remain available for daily-free eligible actions. Errors reading debt state are propagated.

7. **Operator goodwill** (`ApplyGoodwillAdjustment`): only an account with a `pricing_admin_grants` row; waives matching active debt (all of the owner's, or one source), creates no spendable credits; no match is an error and writes nothing. Audit (SHA-256 actor hash, owner pseudonym, reason code, private free-text notes, debt ids, waived credits) lives only in `credit_goodwill_audit`; the analytics outbox gets `entitlement_changed` with `reason_code` only. There is no HTTP surface yet: operators run it from an operator tool/console under their own account, which is the runbook below.

   *Runbook*: (1) confirm the debt in `credit_debt_history` (status `active`) and the provider decision; (2) call `ApplyGoodwillAdjustment(actor, owner, sourceID|0, reasonCode, notes)` as a granted operator; (3) verify the audit row and zero active debt; never put customer details in analytics or tickets.

7. **Data Retention & Privacy**:
   - New tables `credit_reversals`, `credit_refund_allocations`, `credit_dispute_keys`, `credit_debt_history` are retained 7 years in financial backups.
   - On account deletion, `learner_id` is set to NULL while `owner_reference` is pseudonymized (SHA-256 hash).
   - Restore-drill anonymizer treats identifiers/enumerations as safe and `credit_goodwill_audit.notes` as private free text (anonymized).
