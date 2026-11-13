## MODIFIED Requirements

### Requirement: Restore drill
A restore drill MUST restore the newest dump through the restorer identity into an isolated throwaway database, anonymize it before any other access, apply account-deletion tombstones, rebuild Redis from Postgres, and run schema (migration registry), data, object-reference and privacy checks. It MUST measure RPO (age of the newest dump by key date, limit 24 hours) and RTO (drill duration, limit 4 hours) and report the result. The ready-PR pipeline SHALL run it on a migrated synthetic dump and fail `ci-ok` on any failed check. A scheduled drill SHALL run only against configured storage and SHALL skip with a notice, not fail, when owner credentials are absent. Restored output MUST contain no email address that is not an anonymized replacement, and any text column not classified by the anonymizer registry MUST fail the drill.

#### Scenario: Drill passes
- **WHEN** the drill runs on a synthetic dump
- **THEN** the restore completes, every check passes and `restore_drill_completed` is emitted with `ok` true and `source` ci

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
