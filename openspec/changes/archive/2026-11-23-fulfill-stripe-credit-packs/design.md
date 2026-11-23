# Design and conservative defaults

Existing immutable orders remain the financial terms. Separate immutable Session bindings, inbox receipts and processing outcomes preserve the audit trail. The provider receives configured synthetic mappings only in test mode, order identity, pseudonymous learner binding and idempotency key. No real keys, network payment adapter or live enablement is supplied. Native channels and subscriptions retain E7-04 behavior.

Verify the exact raw body with Stripe-Signature v1 HMAC-SHA256 over timestamp + '.' + body, constant-time comparison and a non-disableable five-minute past/future tolerance. Verify before storing anything. Store only a redacted event envelope, separately expiring after 30 days; immutable receipt/dedupe references last seven years. Never retain customer details or arbitrary metadata.

Durable inbox work retries across process restarts. Canonical Session retrieval, order/owner/environment/price/amount/currency validation and an order lock precede the purchase transaction. The environment-scoped PaymentIntent business key and per-event outcome uniqueness protect the ledger and outbox. Paid outcomes win over late failure/expiry. Unsupported events are acknowledged without financial effects. A deleted account retains references without regaining identity or Credits.

An authentic notification that precedes the Session binding transaction waits in the inbox. Unmatched receipts do not occupy the bound-work batch, and become eligible once the immutable Session binding exists.

Defaults pending owner review: test-only injected provider; reject live even when requested pending E7-05; fixed five-minute signature window; no external URL in fixture UI; explicit status refresh; pending work never auto-resumes AI. A completed unpaid Session stays pending; async failure and expiry require canonical unpaid state, and canonical paid state wins. Provider outages retain inbox work for retry. Financial snapshots use existing encrypted 35-day retention; all payment references are retained seven years. No AI behavior changes: existing eval/fallback gates remain.

Official Stripe references checked 2026-11-09:
- https://docs.stripe.com/api/checkout/sessions/create
- https://docs.stripe.com/checkout/fulfillment
- https://docs.stripe.com/webhooks?verify=verify-manually
- https://docs.stripe.com/events/manage-webhook-endpoints
- https://docs.stripe.com/api/events/types
