# Backup and recovery

E11-01 adds encrypted nightly backups to the API `worker`, following ADR
[0003](../../docs/adr/0003-data-stores.md). Local synthetic backup and recovery
are executable without cloud secrets. Production key provisioning, automated
retention and scheduled production restore drills remain separate E11 tickets.
The authoritative policy is the [data-backup spec](../../openspec/specs/data-backup/spec.md).

The worker:

- Runs at **02:00 UTC**, catching up on startup/leader changes. River persists the
  intended day and allows three total attempts on a separate queue with one concurrent
  backup per process. A terminal failure produces a structured error alert.
- Uses **pg_dump 18.6** from a digest-pinned Postgres image and **age 1.3.2**.
  The worker image runs as UID 65532. Only the public X25519 recipient is needed;
  recovery private keys stay outside the worker and repository.
- Streams the custom archive directly into age encryption; only ciphertext is
  staged in a 0600 temporary file. All return paths remove it. A single conditional
  S3 PutObject publishes `YYYY-MM-DD/schema-<Goose version>.dump.age`. Completed
  dates are skipped, concurrent publications preserve the first complete object,
  and failed uploads never expose partial objects or multipart uploads. Staging
  is limited to 4 GiB; larger databases need a separately designed multipart flow.
- Uses `BACKUP_S3_*` settings for the separate `duolify-backups` bucket.
  Development initialization provisions a writer with GetObject/PutObject and
  ListBucket, explicit delete/admin denial, and an application identity confined
  to its own bucket. RustFS root credentials reach only initialization. Retention
  administration is separate: general backups **14 days**, financial backups
  **35 days** per ADR 0011; production lifecycle/snapshots must still be configured.
- Emits `backup_completed` / `backup_failed` with size, duration and outcome via
  the existing non-blocking PostHog wrapper. The reserved operations UUID is
  `00000000-0000-4000-8000-000000000011`, platform=web, source=api, app_version from
  build information. The shared consent resolver explicitly recognizes this non-learner operations identity; all learner identities require current stored consent. `ANALYTICS_OPT_OUT=true` suppresses delivery. No database
  contents, connection strings, keys or provider diagnostics enter events.

## Local setup and verification

1. Run `mise run infra:build-worker` to build the worker and initializer images.
2. Generate an age identity into a protected file **outside the repository**:
   `mise exec -- docker run --rm --entrypoint age-keygen duolify-worker:local`.
   Store the private key with the recovery operator. Put only its `age1...`
   public recipient into `BACKUP_AGE_RECIPIENT` in your untracked `.env`.
3. Run `mise run infra:test` to start healthy dependencies and initialize the
   separate application/writer IAM policies. For existing local volumes, root
   credentials now use `S3_ROOT_*`; application keys remain `S3_*`. No data-volume
   reset is necessary. Never use the local fixture credentials in cloud.
4. Start the stack with workers using
   `mise exec -- docker compose --profile workers up -d --build --wait`.
   Missing/invalid recipients or scoped credentials fail worker startup.
5. Run `mise run api:test-e2e` and `mise run api:test-integration`. E2e builds and
   launches the real worker image, then decrypts/restores its synthetic learner
   and ledger backup and checks HTTP health. Integration exercises River retries,
   real RustFS encrypted uploads and denied deletion by writer/API identities.
   Both suites use disposable containers, random service ports and a local
   PostHog HTTP double; no real provider calls or configured databases are used.

When another stack owns standard ports, set `COMPOSE_PROJECT_NAME` and override
`POSTGRES_PORT`, `REDIS_PORT`, `S3_PORT`, `S3_CONSOLE_PORT` for mise infra tasks.
Host-side database/storage URLs must use the chosen ports; internal service DNS
and ports remain fixed. E2e's packaged worker uses Docker host networking on the
Linux local/CI runner to reach only the disposable services' random host ports.

## Retention, identities and object protection (E11-03)

Scope under ADR 0012: enforced and tested against the local RustFS store only.
Railway snapshots/PITR, production buckets and key custody are owner-provided
(E11-02 #27 stays human-gated); nothing here calls Railway or any cloud.

- **Retention split.** With `FinancialCopy` (always on in the worker) each nightly
  run publishes the same age ciphertext twice: `financial/<date>/schema-<n>.dump.age`
  first, then `<date>/schema-<n>.dump.age`. General keys expire after **14 days**,
  `financial/` keys after **35 days** (a dump dated D is kept through D+N days). Live
  seven-year financial history in Postgres is never touched. `BACKUP_GENERAL_RETENTION_DAYS` /
  `BACKUP_FINANCIAL_RETENTION_DAYS` may only raise the windows. `account-deletions/`
  and unknown keys are never listed or deleted.
- **Newest-dump guard.** Per class, the expirer never deletes the newest dump by key date,
  even past its window, so a stalled writer (worker broken 15+ days) cannot leave zero
  restorable backups. When it holds one back it logs a warning with a count only (no keys);
  treat that log as a signal the writer stopped.
- **Separate expiry job.** `cmd/backup-expire` (`bin/backup-expire`) is a one-shot job
  run by a scheduler under the expirer identity; the writer keeps no delete API.
  Failures emit `backup_failed` (no keys or errors; honors `ANALYTICS_OPT_OUT`);
  success is logged only. Railway scheduling is E11-06.
- **Identities (key references only; values live in `.env`/secret manager).**

  | Identity | Env | Rights |
  | --- | --- | --- |
  | Writer | `BACKUP_S3_ACCESS_KEY_ID` | list/get/put backup bucket; delete and policy denied |
  | Expirer | `BACKUP_EXPIRE_S3_ACCESS_KEY_ID` | list + delete `20*`/`financial/*` only; no get/put; ledger delete denied |
  | Restorer | `BACKUP_RESTORE_S3_ACCESS_KEY_ID` | read-only backup bucket |
  | Application | `S3_ACCESS_KEY_ID` | get/put/delete-marker on application bucket only; no version delete, no versioning or policy change |
  | Asset recovery | `ASSET_RECOVERY_S3_ACCESS_KEY_ID` | list versions, get, put on application bucket; no delete |

  Rotate by re-running `mise run infra:deps` with new values (regeneration is tracked
  in the operator's change record, not in the repo).
- **Buckets.** The backup bucket (`duolify-backups`) is distinct from the application
  bucket and unversioned so expiry really deletes; its contents are age-encrypted
  before upload (key: `BACKUP_AGE_RECIPIENT`, private key held by the recovery operator).
  The application bucket has versioning **on** (`infra/s3/init-buckets.sh` and `backup-init`).
  Verify server-side encryption and versioning on the Railway bucket before real assets.
- **Overwritten-object recovery.** `backup.Recovery` re-publishes a previous version
  (`RestoreVersion`) or operator-generated bytes (`Regenerate`) as a **new version under
  the same key**, so stored references and URLs keep working and history stays intact.
  A job id (`^[a-z0-9][a-z0-9-]{7,63}$`) is mandatory and recorded in object metadata
  (`duolify-recovery-job`, `-kind`, `-source-version`) and an operational log line; nothing
  regenerates silently. Failures emit `backup_failed`.

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
   error. This standalone script does not send analytics; the automated drill below does.
6. Remove the decrypted file securely according to the host's storage policy.

### Automated anonymized drills (E11-05)

`internal/backup` (`RunDrill`) automates the steps above; `infra:restore-drill` stays
as the manual path for an already anonymized archive.

- **Ready PR** (`restore-drill` job, `mise run api:restore-drill-ci`, part of `ci-ok`): the fixture
  migrates Postgres 18 to the current schema, seeds synthetic data, backs it up through
  the real worker path (age, RustFS) and restores it with the read-only restorer identity
  into a throwaway database (`DockerRestorer`, `docker exec`, dump on stdin). Any failed
  check fails the job. Tests prove it with injected corruption (missing migration,
  lost application object, unclassified text column carrying an email, corrupt ciphertext).
- **Order**: decrypt in memory → restore → **anonymize** (`Anonymize`; credentials, sessions
  and webhook payloads purged, identities and learner-authored text replaced) → schema,
  migration-registry and ledger checks → deletion tombstones from `account-deletions/v1/`
  (read-only; a drill never writes to backup storage) → Redis rebuild from Postgres
  (`gamification.RebuildLeagueCache`) → feature and object-reference checks → privacy checks.
- **Privacy checks**: every text, jsonb and array column must be registered in the
  anonymizer (`piiRules`, `purgedTables`) or reviewed as safe (`reviewedSafe`); an
  unclassified column from a new migration fails the drill until classified. A scan then
  fails on any non-`@anonymized.invalid` email and on known synthetic PII values.
- **Measures**: RPO is now minus the newest general dump's key date (limit 24h; stale or
  missing dumps fail before restore). RTO is the drill duration (limit 4h). Both are in the
  `Report`; `restore_drill_completed` carries `ok` and `duration_ms` (`source` is `ci` or
  `scheduled`) and a failure also emits `backup_failed`. Opt-out suppresses both. Errors,
  keys and rows are never sent or printed.
- **Scheduled** (`restore-drill-scheduled.yml`, `mise run infra:restore-drill-scheduled`): runs
  `cmd/restore-drill` against the configured backup bucket with throwaway loopback
  Postgres/Redis (`DRILL_PG_PORT` 4112, `DRILL_REDIS_PORT` 4113 by default). Without
  `BACKUP_RESTORE_S3_*` and `BACKUP_AGE_IDENTITY` it prints a skip notice and exits 0.
  It is exercised against fixtures only in this repository; production data, keys and
  storage remain owner-provided (ADR 0012).
- **Extension point**: `DrillOptions.FinancialReplay` is where E7-11's quarterly financial
  replay plugs in. No business checks are bundled; the report says `not-configured`.
- **Register checks as migrations arrive**: add feature invariants to `DrillOptions.Checks`
  (`ObjectReferences` helper for object keys) and classify new text columns in `anonymize.go`.

## Account deletion ledger

Both API serve and worker startup require the scoped `BACKUP_S3_*` configuration. `account-deletions/v1/<SHA-256>.json` is the authoritative append-only deletion ledger in the backup bucket, separate from restored database snapshots. Each atomic conditional Put contains only the hashed learner reference. The write must succeed before SQL erasure commits; retries preserve the first complete object. If SQL fails after a durable intent, retry/startup replay completes that intent. Never expire, overwrite, delete or restore this prefix; scope 14/35-day retention to dated archives only. Keep the writer without delete/admin permission.

After restoring a dump, start the API against the existing ledger bucket. Before opening HTTP listeners, `ReplayDeletionLedger` migrates legacy SQL tombstones to object intents, checks every restored learner hash against storage, imports matching intents and reapplies full erasure/financial pseudonymization. A storage/replay failure stops startup. Do not expose the restored database through another service until this replay succeeds. The integration fixture restores an actual encrypted pre-deletion dump with no SQL tombstone and proves the independent object intent keeps the learner deleted.
