# Enforce account privacy at shared boundaries

## Why

Issue #163 found ungated analytics emitters, deletion records lost by restoring an older database, and exports/deletions authorized by old sessions.

## What Changes

- Fail closed at the shared analytics client using injected consent; cache denials briefly while applying opt-outs immediately.
- Persist deletion intents as immutable hashed references in the existing backup object store before SQL erasure. Replay external intents before opening listeners after startup or restore.
- Require the existing 15-minute recent-auth session policy and per-learner export/deletion budgets. Offer fresh sign-in recovery in AccountPrivacy and preserve passphrase encryption.

## Data backup

The object ledger is independent of database snapshots and excluded from backup expiry/restore operations. SQL tombstones are a replay cache. Recovery tests restore a real pre-deletion encrypted dump and reapply object-ledger erasure.

## Analytics

No new events. Existing lesson, retry, credit and privacy-control events require consent; no learner content is sent. Operational monitoring remains independent of analytics delivery.

## Model testing and reliability

No AI behavior changes. Existing model evals and fallback policies remain unchanged.
