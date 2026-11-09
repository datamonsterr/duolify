## Defaults chosen for E7-07

Only explicitly injected test providers activate monthly plans. Annual/native
activation, lifecycle changes, refunds and live credentials remain under their
existing tickets/gates. Pending monthly checkout prevents another monthly
checkout until verified failed/expired; a retry with the same request is allowed.

Accept only a single quantity-one, non-prorated monthly line with configured
price, currency and exact amount, positive Stripe-paid funds, no out-of-band
payment, no discount/credit/tax adjustment and no trial. Billing reason must be
subscription_create or subscription_cycle. Invoice line period is authoritative,
not invoice header period; canonical subscription item period must cover it.
Month anniversaries use the original billing anchor and clamp at UTC month end
(January 31 → February 28 → March 31). The immutable Pricing version also
identifies the entitlement version. Benefits and concurrency are snapshotted.

Checkout and customer.subscription events can bind/observe but cannot fund or
extend a term. An old lifecycle event cannot erase funded access. A future term
is rejected for manual reconciliation rather than granting credits early.
Funded monthly terms determine access by database time on every read, independent
of the wallet cache. New overlapping paid terms fail closed even across orders.
Issued credits never expire. Fixture management uses an internal explanatory
screen; no real billing portal or network payment action is enabled.
The purchase event is unique per subscription; allowance and entitlement events
are unique per funded service month. Plan concurrency uses the funded snapshot
and expires with access, independently of the non-expiring issued balance.

## Current official provider references (consulted 2026-11-09)

- https://docs.stripe.com/billing/subscriptions/webhooks
- https://docs.stripe.com/api/events/types
- https://docs.stripe.com/api/invoices/object
- https://docs.stripe.com/api/invoice-line-item/object
- https://docs.stripe.com/api/subscriptions/object
- https://docs.stripe.com/changelog/basil/2025-03-31/deprecate-subscription-current-period-start-and-end
- https://docs.stripe.com/webhooks

invoice.paid also covers marked-paid/out-of-band outcomes; invoice.payment_succeeded
indicates a successful attempt. Both trigger canonical reconciliation. Subscription
periods are item-level in current Stripe APIs. No Stripe SDK/network API ships here.
