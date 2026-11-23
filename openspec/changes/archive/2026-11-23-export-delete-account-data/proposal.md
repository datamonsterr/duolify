# E1-07: Export and delete account data

## Why

Learners need complete, portable access to their persisted account data and a reliable way to erase personal data without rewriting legally retained financial history. Restores must continue honoring completed deletion requests.

## What Changes

- Add a complete account export for every current learner-owned table, excluding authentication secrets and hashes; produce an encrypted, time-limited download.
- Add account deletion with explicit confirmation, session revocation, immediate personal-data erasure, pseudonymous financial retention, restore tombstones and a PostHog person-deletion request.
- Add a registration point and coverage check so future learner-owned tables must declare export and deletion behavior.
- Match S23 desktop and mobile settings privacy controls, clear local credentials and disposable caches, and add stable privacy-control analytics.

## Data and backup

Deletion tombstones are backed up and reapplied before a restored database serves traffic. General backups expire after 14 days; financial snapshots after 35 days. Credit ledger rows remain append-only and retain a pseudonymous subject for at least seven years. Export artifacts are encrypted and expire after seven days. Update the data inventory and restore fixture.

## Model reliability

No AI behavior changes. Existing evaluation and fallback gates remain unchanged.
