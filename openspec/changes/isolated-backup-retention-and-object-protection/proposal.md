# Isolated backup retention and object protection (E11-03)

## Why
Retention was documented but not enforced, and overwritten Slide or asset objects
had no tested recovery path. ADR 0012 (row E11-02) limits this ticket to the local
S3-compatible store (RustFS) with versioning on; Railway snapshots/PITR and
production key custody stay owner-provided.

## What Changes
- A separate expiry job and identity deletes general dumps after 14 days and
  `financial/` dump copies after 35 days. The append-only writer keeps no delete API.
  Live seven-year financial history in Postgres is untouched.
- Scoped identities: writer, expirer, read-only restorer, application (no version
  delete or versioning change) and asset-recovery operator.
- Tracked restore or regeneration of overwritten application objects as a new
  version under the same key, so references keep working.
- Analytics: existing `backup_failed` reports retention and recovery failures.

## Impact
API backup package, local initializer, infra docs, data inventory. No AI behavior,
no new analytics events, no cloud calls. E11-05 drills and E11-06 Railway config are out of scope.
