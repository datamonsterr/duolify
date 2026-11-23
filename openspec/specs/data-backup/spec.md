# data-backup Specification

## Purpose
Protect Learner data with nightly dumps, retention, a tested restore drill, and a per-feature data inventory that every spec keeps current.

## Requirements

### Requirement: Nightly dumps
The API `worker` SHALL run `pg_dump` nightly and upload the result to object storage. Each dump MUST be encrypted and named with date and schema version. Success or failure MUST emit `backup_completed` or `backup_failed`.

#### Scenario: Nightly run
- **WHEN** the nightly job runs
- **THEN** an encrypted dump appears in the backup bucket and `backup_completed` is emitted

#### Scenario: Dump fails
- **WHEN** the dump or upload fails
- **THEN** the job retries and then raises an alert

### Requirement: Retention
General dumps MUST be kept for 14 days and financial dumps for 35 days. Expiry
MUST run as a job under an identity separate from the append-only backup writer,
and MUST NOT delete the newest dump of either class or the account-deletion ledger or shorten live seven-year
financial history. WAL archiving for point-in-time recovery and an off-site disaster recovery copy SHALL provide a second layer. Backups MUST live in a different encrypted bucket from application data, with no application delete rights.

#### Scenario: Old dumps
- **WHEN** a general dump is older than 14 days or a financial dump older than 35 days
- **THEN** the separate expiry job removes it and keeps newer dumps and the deletion ledger

#### Scenario: Stalled writer
- **WHEN** every dump of a class is past its window because the writer stopped
- **THEN** the expiry job keeps the newest dump of each class (by key date) and logs a count without keys

#### Scenario: Writer cannot delete
- **WHEN** the backup writer or application identity attempts to delete a dump
- **THEN** storage denies the request

### Requirement: Restore drill
A restore drill MUST restore the newest dump through the restorer identity into an isolated throwaway database, anonymize it before any other access, apply account-deletion tombstones, rebuild Redis from Postgres, and run schema (migration registry), data, object-reference and privacy checks. It MUST measure RPO (age of the newest dump by key date, limit 24 hours) and RTO (drill duration, limit 4 hours) and report the result. The ready-PR pipeline SHALL run it on a migrated synthetic dump and fail `ci-ok` on any failed check. A scheduled drill SHALL run only against configured storage and SHALL skip with a notice, not fail, when owner credentials are absent. Restored output MUST contain no email address that is not an anonymized replacement, and any text column not classified by the anonymizer registry MUST fail the drill.

#### Scenario: Drill passes
- **WHEN** the drill runs on a synthetic dump
- **THEN** the restore completes, every check passes and `restore_drill_completed` is emitted with `ok` true and `source` api

#### Scenario: Drill fails
- **WHEN** the restore fails, a migration is missing or any check fails
- **THEN** CI fails, `backup_failed` and `restore_drill_completed` with `ok` false are emitted and no error text, key or row content is sent

#### Scenario: Stale or missing dump
- **WHEN** the newest dump is older than 24 hours or none exists
- **THEN** the drill fails on RPO before restoring

#### Scenario: Deleted learner in an old dump
- **WHEN** the restored dump predates an account deletion recorded in the object ledger
- **THEN** the learner is absent from the drill output

#### Scenario: No owner credentials
- **WHEN** the scheduled drill runs without restorer credentials or age identity
- **THEN** it prints a skip notice and exits successfully

#### Scenario: Financial replay hook
- **WHEN** a financial replay hook is supplied by E7-11
- **THEN** it runs after the other checks; without it the report states `not-configured`

### Requirement: Object storage protection
Object storage for Slides, assets and mirrored Golden set revisions MUST use versioning where supported. Protected assets MUST have encrypted backup copies pinned by SHA-256 checksums; golden set revisions mirrored to S3 MUST be verified with SHA-256. On R2, backup `wal/` and `base/` prefixes use bucket lock rules and expiry; the store cannot recover overwritten object versions, and version-aware asset recovery and deletion-ledger versioning are not guaranteed. These limitations MUST be revisited before real assets or the deletion ledger rely on object versions. Slides MUST be regenerated only by a tracked operator job, never silently. Where versioning is available the application identity MUST NOT change versioning, and recovery and regeneration SHALL append a version tagged with the operator job id.

#### Scenario: Object overwritten on a versioned store
- **WHEN** a Slide object is overwritten by mistake on a versioned store
- **THEN** an operator job restores the previous version under the same key and the same reference serves it

#### Scenario: Protected asset copy on R2
- **WHEN** a protected asset is backed up on R2
- **THEN** its encrypted copy is pinned by checksum and records `unversioned`; overwritten versions cannot be recovered from R2 version history

#### Scenario: Untracked regeneration
- **WHEN** regeneration is requested without a job id
- **THEN** it is refused and nothing is written

### Requirement: Per-feature data inventory
Each spec MUST list what data it creates, where it is stored, how it is backed up and when it is deleted. The inventory MUST be kept in `docs/data-inventory.md` and a change that adds data MUST update it.

#### Scenario: New table
- **WHEN** a change adds a table
- **THEN** the pull request updates the inventory and covers the table in the restore drill checks

### Requirement: Recovery objectives
The system SHALL target a recovery point of 24 hours and a recovery time of 4 hours for Postgres. Redis MUST NOT be a backup target because it is reconstructible.

#### Scenario: Redis lost
- **WHEN** Redis is wiped
- **THEN** counters and leaderboards are rebuilt from Postgres

### Requirement: Deletion and privacy
Account deletion MUST remove Learner data from Postgres within 30 days and from backups through retention roll-off. Backups MUST NOT be restored into non-production environments without anonymization.

#### Scenario: Restore to dev
- **WHEN** a dump is used in the dev environment
- **THEN** personal fields are anonymized first

### Requirement: Backup analytics events
Backup and drill jobs SHALL emit `backup_completed`, `backup_failed` and `restore_drill_completed` with `size_bytes`, `duration_ms` and `ok`, as defined in the `analytics` spec.

#### Scenario: Drill reported
- **WHEN** a drill finishes
- **THEN** `restore_drill_completed` is emitted

### Requirement: Backup tooling reliability
Backup jobs have no AI component, so no model reliability requirement applies. Backup jobs MUST be idempotent and MUST be covered by an integration test with Postgres and a local S3-compatible store (RustFS).

#### Scenario: Job retried
- **WHEN** a dump job runs twice for the same date
- **THEN** the second run replaces or skips and no partial file remains

### Requirement: Synthetic worker backup slice
The worker SHALL run nightly at 02:00 UTC, persisting the intended UTC date in
River, and encrypt a Postgres 18 custom archive with an age public recipient
before uploading a complete date/schema-named object to a separate bucket.
Retrying the date SHALL skip or replace the complete object without partial
objects. The writer MUST lack delete rights; retention belongs to a separate
identity. No plaintext archive SHALL be persisted locally.

#### Scenario: Recover a synthetic database
- **WHEN** a River backup runs against the migrated synthetic learner and ledger
- **THEN** its RustFS object decrypts and restores schema, learner and ledger data

#### Scenario: Failed publication and retry
- **WHEN** dump or upload fails and River retries the original date
- **THEN** no partial object is visible and the retry publishes one complete dump

### Requirement: Private backup outcome reporting
The worker SHALL emit catalogued backup_completed and backup_failed events via
the existing non-blocking analytics wrapper with the reserved operations UUID,
common properties, size_bytes, duration_ms and ok. Global opt-out MUST suppress
events. Errors and learner text MUST NOT enter properties. No AI is involved.

#### Scenario: Opted out backup
- **WHEN** global analytics opt-out is set and a backup finishes
- **THEN** backup behavior is unchanged and no event is delivered

### Requirement: Backup data inventory
Encrypted objects and date-only River args SHALL be recorded in the inventory,
including separate retention control and focused synthetic recovery checks.

#### Scenario: Restore and deletion permissions
- **WHEN** the local synthetic backup is recovered
- **THEN** migration versions and ledger checks pass and writer deletion is denied

### Requirement: Correlated redacted operational traces
The API SHALL propagate its trusted request ID and span context to gRPC engine
calls and River jobs. Retries SHALL preserve correlation and log distinct attempt
spans without tokens, prompts, private text, object keys or raw errors. Readiness
SHALL distinguish postgres, redis, engine and object_storage failures with safe
states. Backup exhaustion, queue outages and dependency incidents SHALL each
raise one alert per observed incident, independent of analytics availability.

#### Scenario: Follow a failed action
- **WHEN** a request calls the engine and enqueues a backup that exhausts retries
- **THEN** HTTP, RPC and worker spans share request and trace IDs and one terminal alert fires

#### Scenario: Repeated unhealthy readiness
- **WHEN** a dependency fails across repeated probes
- **THEN** readiness names the failing dependency and one incident alert fires until recovery

### Requirement: Bounded operational data
Logs and traces SHALL be redacted and bounded by size rotation. Metrics SHALL
have seven-day collector retention. Dashboards SHALL be derived; operational
configuration SHALL live in Git under infra. The data inventory SHALL document
River correlation metadata, recovery checks and derived-data disposal. No AI
behavior changes; existing evaluation and fallback requirements SHALL remain.

#### Scenario: Restore correlation metadata
- **WHEN** an encrypted synthetic backup is restored
- **THEN** safe River metadata survives and no new learner data store is required

### Requirement: Publication draft data
`publication_drafts` (snapshots and version refs) SHALL be in nightly dumps and the protected asset copy under `Object storage protection`, and `publication_screenings` rows (verdict, cost, codes only) SHALL be retained one year. Neither holds private learner payload. Account deletion MUST remove the owner's drafts and screenings and the export MUST list drafts by number, date and verdict. The restore check MUST cover both tables.

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** no draft or screening row references them

### Requirement: Sharing data backup
`courses.visibility`, `publication_links` (hash only, never token plaintext), course draft references MUST be included in nightly dumps. Snapshot assets are covered by existing object storage protection. docs/data-inventory.md MUST be updated.

#### Scenario: Nightly dump
- **WHEN** the nightly backup runs
- **THEN** `courses.course_visibility` and `publication_links` rows (token_hash, created_at, revoked_at, course_id) are included; plaintext tokens MUST NOT appear in any backup log or dump

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** all `publication_links` rows for their Courses are removed and no token hash referencing them remains in the dump

### Requirement: Course copies in backups
`course_copies` (pins) and copied Lesson rows MUST be in the nightly dump and the privacy export; erased with the Learner's account. A copy MUST survive deletion of the source Course or its owner. docs/data-inventory.md MUST list the table.

#### Scenario: Restore
- **WHEN** Postgres is restored
- **THEN** every copy resolves its pinned version and lessons without the source Course

### Requirement: Copy-local generated Lessons are backed up
Lessons generated in a Course copy and their consent records SHALL be included in the nightly encrypted Postgres dump and the protected asset copy under `Object storage protection`. Restore MUST return them with the copy and the immutable published hash; account deletion MUST erase them.

#### Scenario: Restore keeps copy-local content
- **WHEN** a dump with a generated copy Lesson is restored
- **THEN** the Lesson and its consent record exist and the pin hash is unchanged

### Requirement: Railway environment separation and private engine isolation
The system SHALL provide separate dev, staging and production configurations for the self-hosted stack. Secret values MUST NOT be committed. The AI Engine MUST remain on private internal networking, without a public route. Postgres, Redis, and separate asset and backup storage roles SHALL be configured.

#### Scenario: Reject public engine
- **WHEN** an environment configuration exposes the engine on a public route
- **THEN** validation rejects the configuration

### Requirement: Release gates and plan-only CI evidence
CI SHALL preserve `ci-ok` and draft/ready tiers. The `deploy-vm` workflow SHALL deploy tested images by digest to the VM, with readiness and migration safeguards. Configuration outputs and errors MUST redact secret values.

#### Scenario: Missing dependency or plaintext secret
- **WHEN** an environment lacks a required dependency or contains a plaintext secret value
- **THEN** validation rejects the configuration and redacts secrets in error output

### Requirement: Marketplace listing metadata is backed up
`marketplace_listings` and `marketplace_listing_stats` SHALL be included in the nightly encrypted Postgres dump. Restore MUST return listings with their search vector inputs, and account deletion MUST erase the owner's listings. Redis holds only derived pages and is not backed up.

#### Scenario: Restore rebuilds browsing
- **WHEN** a dump with a listing is restored and Redis is empty
- **THEN** the catalogue lists the Course and an erased owner's listing is gone

### Requirement: Course invites are backed up and purged
`course_invites` and `course_invite_deliveries` SHALL be in the nightly encrypted Postgres dump. Invites that are accepted, declined, expired or revoked SHALL be purged 90 days later, and account deletion MUST erase invites the Learner sent or received.

#### Scenario: Final invites purge
- **WHEN** an invite resolved more than 90 days ago is swept
- **THEN** it and its delivery rows are gone and a pending invite remains

### Requirement: Versions, pins and archived lessons are backed up
Version lineage and changelogs (`publication_drafts`), pins and lock versions (`course_copies`), lesson keys and archived lessons (`course_lesson_stubs`, `course_units`) SHALL be in the nightly encrypted Postgres dump, and a restore MUST resolve every pin and referenced object.

#### Scenario: Restore keeps archived history
- **WHEN** a dump with an updated copy is restored
- **THEN** the pin, lock version and archived lessons are present

### Requirement: Financial restore replay
The ready pipeline SHALL verify restored balances, paid liability and reserve,
price versions, funded annual schedules, dedupe/outbox and deletion tombstones.
It MUST replay independent canonical TEST provider fixtures once and prove a second
replay is a no-op. A quarterly scheduled financial replay SHALL use the existing
owner-credential gate and print a skip notice when credentials are absent. New
retry state MUST be backed up, classified for anonymization and included in account
export policy without introducing personal text. This feature has no AI behavior;
existing evaluation and fallback gates SHALL remain unchanged.

#### Scenario: Financial restore
- **WHEN** the synthetic dump is restored through local versioned RustFS
- **THEN** financial invariants pass, missing funded operations recover once and duplicate replay changes no financial, inbox or outbox state

#### Scenario: Corrupt financial dump
- **WHEN** a price reference, annual schedule, source allocation or financial outbox invariant is corrupt
- **THEN** the restore drill fails and emits restore_drill_completed with ok false without private row content

### Requirement: Retention and recovery reporting
Failed retention or recovery jobs SHALL emit `backup_failed` with the reserved operations UUID and common properties, honor global opt-out, and never include keys, errors or content.

#### Scenario: Expiry fails
- **WHEN** the expiry job cannot list or delete
- **THEN** `backup_failed` is delivered without object keys

### Requirement: Row retention sweep
Rows that only provide client-retry idempotency SHALL be purged by the worker job `row_retention` (daily, unique per period, batched with `FOR UPDATE SKIP LOCKED`, windows and cadence from the environment variables listed in `.env.example`). The per-feature data inventory MUST state each purged table's window. Purging live rows MUST NOT change dump retention, restore checks or account-deletion cascades.

#### Scenario: Inventory current
- **WHEN** a table gains a retention window
- **THEN** `docs/data-inventory.md` names the window, the env variable and the sweep
