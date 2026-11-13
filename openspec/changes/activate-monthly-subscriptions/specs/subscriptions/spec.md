## ADDED Requirements

### Requirement: Funded monthly fixture activation
The API SHALL reconcile signed invoice.paid and invoice.payment_succeeded receipts
using canonical provider evidence and immutable configured order terms. It MUST
atomically append invoice/entitlement history, one subscription_grant and the
deduplicated plan_purchased, subscription_grant_issued and entitlement_changed
outbox events per funded service-month key. Checkout and subscription events
alone MUST NOT grant. Invalid, unpaid, trial, wrong-account or overlapping terms
MUST NOT activate. Only injected test providers SHALL be available until E7-05.

#### Scenario: Reordered duplicated funding
- **WHEN** checkout, invoice and subscription events arrive in either order or concurrently
- **THEN** one funded term, one allowance and one occurrence of each event exist

### Requirement: Synchronized funded plan display
Wallet and Shop SHALL expose configured plan, preserved Pricing/entitlement
version, paid-through, benefits and channel management on desktop and mobile.
Access MUST use current funded periods from Postgres, independent of cached balance.
New overlapping monthly checkouts MUST be refused. Existing credits SHALL remain
after term expiry and management MUST NOT start paid work automatically.

#### Scenario: Another device after expiry
- **WHEN** another authorized device reads a wallet after the funded term ends
- **THEN** paid benefits are inactive and issued credits remain unchanged

### Requirement: Financial history privacy and recovery
Subscription history MUST be append-only. Subscription bindings, invoices, funded terms, entitlement snapshots and business
keys SHALL be append-only, retained seven years and included in nightly encrypted
financial backups with 35-day snapshot rotation. Export SHALL include owned history;
account deletion and restored tombstones SHALL sever identity via the original
order. Analytics MUST inherit UUID/common properties, opt-out and content-free
allowlists; outbox failure MUST roll back funding. No new AI behavior SHALL change
existing evaluation, quote, budget or fallback gates.

#### Scenario: Restore and replay
- **WHEN** a restored paid invoice is replayed after identity deletion
- **THEN** the grant and audit history survive without identity or duplicate funding
