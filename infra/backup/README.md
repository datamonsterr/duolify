# Backup and recovery

This is a restore-tooling bootstrap, **not an active backup service**. ADR
[0003](../../docs/adr/0003-data-stores.md) assigns nightly backups to the API
`worker`; its current dummy River worker does not run `pg_dump` or upload files.
The authoritative policy is the [data-backup spec](../../openspec/specs/data-backup/spec.md).

The follow-up worker job must:

- Run Postgres 18 `pg_dump --format=custom` nightly, encrypt before upload and name
  objects by UTC date and schema version. Retry idempotently without partial files.
- Use the separate `backups` bucket with credentials unavailable to the API.
  Restrict the upload identity from deleting backups; use a separate retention
  identity to expire dumps after **14 days**. Keep Railway volume snapshots too.
- Emit `backup_completed` / `backup_failed` with size, duration and outcome through
  PostHog; alert after retries fail. No AI or model eval is involved.
- Cover dump, encrypted upload, retention and retries with Postgres + local S3
  integration tests. Add the required dump tooling and encryption key management
  to the worker image (the bootstrap distroless image contains neither).

Assets belong in `lesson-assets` on Railway (`duolify` locally), never in the
backup bucket. Compose enables local asset versioning. Verify Railway's bucket
versioning capability before storing real lesson assets; the current IaC SDK
only declares bucket region. Do not treat an undeclared versioning policy as
enabled. Track any regeneration as a job.

Recovery targets: Postgres RPO **24 hours**, RTO **4 hours**. Redis is disposable
and must be rebuilt from Postgres. Account deletion removes personal rows within
30 days and backup copies through retention roll-off; financial ledger retention
is governed by the credits spec.

## Restore drill

1. Select the latest successful encrypted dump and record date, schema version,
   checksum and size. Download using a recovery identity; decrypt in a restricted
   temporary directory. Never commit dumps or keys.
2. Anonymize personal fields before any non-production use. This script accepts
   only an already decrypted, anonymized custom-format archive. Do not give it a
   production dump directly.
3. Run from the repository root:

   ```sh
   mise run infra:restore-drill -- /absolute/path/to/anonymized.dump
   ```

   The script restores into an isolated Postgres 18 container without networking
   or published ports. It checks Goose and River migration metadata, learner and
   credit tables, and ledger relationships. Cleanup removes the container and
   its anonymous volume even after failure. The source database is untouched.
4. Compare restored migration versions with the current application and River
   versions before promoting a recovery. Add per-feature counts, object-key checks
   and ledger balance reconciliation as features arrive; the bootstrap sanity
   query alone is not a complete business-data recovery check.
5. Record duration, schema version, checks and result. The CI integration must emit
   `restore_drill_completed` (`duration_ms`, `ok`, `source=ci`) and fail/alert on
   error. This standalone script does not send analytics.
6. Remove the decrypted file securely according to the host's storage policy.

The ready-PR pipeline should create a **synthetic** migrated fixture, dump it and
call this task now that the first real migration exists. A scheduled production
drill, anonymization tooling, worker scheduling, encryption and alerting remain
follow-up work; see [CI](../../docs/ci.md).
