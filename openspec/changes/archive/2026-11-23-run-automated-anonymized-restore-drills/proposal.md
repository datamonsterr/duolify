# Run automated anonymized restore drills (E11-05)

## Why
The restore script only accepted a hand-anonymized archive and ran bootstrap sanity
queries. Recovery evidence must be automatic, repeatable and privacy-safe. ADR 0012
(row E11-02) limits this to the local RustFS store and CI; production storage and
key custody stay with the owner.

## What Changes
- Ready-PR pipeline job `restore-drill`: a migrated synthetic dump goes through the
  real backup path (age encryption, RustFS, read-only restorer identity, in-memory
  decryption, isolated throwaway Postgres 18) and must pass schema, data,
  object-reference and privacy checks, or `ci-ok` fails.
- Anonymizer and `cmd/restore-drill` for the scheduled real-data drill: anonymize
  before any other access, apply deletion tombstones from the object ledger,
  rebuild Redis from Postgres, verify no PII, measure RPO (24h) and RTO (4h). The
  scheduled job skips with a notice when owner credentials are absent.
- Analytics: `restore_drill_completed(ok, duration_ms)` and `backup_failed` on
  failure, `source=ci` or `scheduled`, via the existing allowlisted wrappers.
- Extension point for E7-11's quarterly financial replay (no business checks here).

## Impact
API backup package, `cmd/restore-drill`, CI, infra docs, data inventory. No AI
behavior, no new tables, no new analytics events, no cloud calls.
