# E7-11: Reconcile and restore financial operations

## Why
Lost provider notifications and restored snapshots must converge without issuing
paid Credits or reversals twice. Inbox retries currently have no terminal bound.

## What Changes
- Bound inbox attempts to five with durable dead letters and explicit replay.
- Reconcile canonical injected TEST provider objects through existing verification,
  fulfillment and business-key deduplication paths; alert on unresolved differences.
- Verify restored financial invariants, replay canonical fixtures and prove a
  second replay changes no financial, inbox or outbox rows.
- Run payload/derived-analytics expiry in an independent daily maintenance job.
- Extend the analytics catalogue and the scheduled quarterly replay workflow.

## Impact
API credits, workers, backup drill, privacy inventory and focused acceptance tests.
One forward Goose migration, 20261115110702, after main's 20261115101701 and
E7-10's controller-renamed 20261115110701. No AI behavior or live billing.
Seven-year financial history and existing encrypted 35-day financial copies remain
retained. Local versioned RustFS is the only storage used for development/tests.

## Conservative default
Five attempts with exponential minute backoff capped at one hour; dead letters
require explicit replay. Reconciliation scans a paginated canonical TEST fixture
inventory, never infers funds from a notification or browser state. Scheduled
financial replay requires independently signed canonical fixture input.
