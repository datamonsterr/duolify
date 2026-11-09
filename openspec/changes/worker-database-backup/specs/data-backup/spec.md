## ADDED Requirements

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
