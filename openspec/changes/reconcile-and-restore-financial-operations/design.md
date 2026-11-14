# Design

An additive retry-state table references immutable inbox receipts. Attempts are
claimed durably before provider calls. A lease prevents concurrent workers from
burning the same retry; crashes consume an attempt. Dead-letter replay opens a
new bounded cycle and preserves its replay count. Money continues to commit under
the existing financial lock with business-key constraints.

Canonical sessions, invoices and subscription snapshots produce content-addressed
observations. Reconciliation checks their business outcomes and annual schedules;
unchanged canonical state adds no receipt on another replay. Provider outages,
rejected evidence and mismatches raise the existing operational dependency alert
and a content-free payment_reconciliation_failed event through analytics wrappers.

The financial drill checks balances, source allocations, paid liability/reserve,
immutable catalogue references, annual schedules, outbox keys and deletion
tombstones before and after replay. It fingerprints retained financial rows after
the first pass and requires the second pass to be identical. Fixtures are supplied
independently of restored data. Quarterly scheduled jobs retain the owner credential
skip gate and require signed TEST fixture input. No cloud or live provider is added.

Raw envelopes expire at 30 days; derived analytics properties expire at
90 days while their financial event keys remain. The purge is a separate durable
job and cannot delete financial tables. All new state is encrypted in the existing
nightly dump and financial copy and covered by the anonymizer and privacy inventory.
