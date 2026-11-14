## ADDED Requirements

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
