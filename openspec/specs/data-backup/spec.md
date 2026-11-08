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
Dumps MUST be kept for 14 days. Railway volume snapshots SHALL be kept as a second layer. Backups MUST live in a different bucket from application data, with no application delete rights.

#### Scenario: Old dumps
- **WHEN** a dump is older than 14 days
- **THEN** a cleanup job removes it

### Requirement: Restore drill
A restore drill MUST restore the latest dump into a throwaway database, run migrations check and data checks, and report the result. The drill SHALL run in the ready-PR pipeline once the first real migration exists, and on a schedule against the latest production dump.

#### Scenario: Drill passes
- **WHEN** the drill runs
- **THEN** the restore completes, check queries pass and `restore_drill_completed` is emitted with `ok` true

#### Scenario: Drill fails
- **WHEN** the restore fails or a check fails
- **THEN** CI fails and an alert is raised

### Requirement: Object storage protection
Object storage buckets for Slides and assets MUST have versioning enabled. Slides MUST be regenerable only through a tracked job, never silently.

#### Scenario: Object overwritten
- **WHEN** a Slide object is overwritten by mistake
- **THEN** the previous version can be restored

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
Backup jobs have no AI component, so no model reliability requirement applies. Backup jobs MUST be idempotent and MUST be covered by an integration test with Postgres and MinIO.

#### Scenario: Job retried
- **WHEN** a dump job runs twice for the same date
- **THEN** the second run replaces or skips and no partial file remains
