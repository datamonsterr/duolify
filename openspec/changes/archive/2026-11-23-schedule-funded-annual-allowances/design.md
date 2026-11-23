## Implementation defaults
Annual means one full quantity-one recurring `year` interval, with the same exact configured funds, currency, account, price, no-trial/no-proration/no-adjustment evidence as monthly. Only signed test-provider fixtures are enabled; E7-05 still gates live billing. Invoice line periods and canonical subscription item periods supply funding, never invoice-level periods.

The initial paid invoice commits only month zero, the funded term, twelve immutable service-month keys and outbox together. UTC anniversaries derive from the original service start, clamp month end, and stay strictly before paid-through. River sweeps every minute and on start; durable schedules and unique ledger business keys, rather than in-memory timers, drive catch-up. A sweep processes at most 100 months per transaction and continues on subsequent jobs. Failed transactions issue no partial grants. No provider call is needed to reconstruct already funded grants after restart/restore.

Turning off renewal records immutable canonical cancellation for existing annual terms and retains all funded grants/benefits. Re-enabling/other lifecycle transitions remain E7-09. Canonical invoice evidence explicitly confirms full-term refund/dispute revocation through the existing provider projection; signed notifications alone cannot revoke. Revocation and entitlement outbox are atomic, and revocation wins over replayed funding and later sweep jobs. Issued balances remain intact; money allocation, partial refunds and attributable reversals/debt remain E7-10. No partial-refund approval is inferred.

Schedules, cancellations and revocations contain financial identifiers only, inherit order identity severance and seven-year retention, and enter nightly encrypted financial dumps with 35-day rotation. Restore compares nonempty rows and exercises catch-up and revoked terms. Analytics catalogue already defines `subscription_grant_issued` and `entitlement_changed`; emit one grant per service-month key and one entitlement change per revocation, no learner text. Existing UUID/opt-out/nonblocking wrappers apply. No new AI behavior or model testing gate is introduced.

## Sources consulted
- https://docs.stripe.com/billing/subscriptions/billing-cycle
- https://docs.stripe.com/api/invoice-line-item/object
- https://riverqueue.com/docs/periodic-jobs
