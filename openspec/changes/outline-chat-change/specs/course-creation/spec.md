## ADDED Requirements

### Requirement: Quoted immutable sample-outline proposals
The system SHALL quote `outline_chat_change` from versioned Pricing config, display eligibility and require consent before reservation. It SHALL send the owned edited draft/revision and untrusted request to the stateless engine. Insufficient eligible Credits SHALL cause zero engine calls. The immutable proposal SHALL contain validated added, removed, moved or retitled operations and SHALL NOT change the draft on generation. Production prices SHALL remain gated by E12-09/E12-10 approval.

#### Scenario: Deliver then discard
- **WHEN** a learner consents to an eligible quote and a valid proposal is delivered
- **THEN** the existing reservation settles within the quote ceiling once
- **AND** Discard changes no outline and never refunds or starts generation

#### Scenario: Provider failure or cancellation
- **WHEN** bounded evaluated retries and fallback fail before delivery
- **THEN** the reservation is released, the draft and request remain recoverable and reconnect reads the same proposal ID

### Requirement: Fresh atomic proposal decisions
Apply SHALL compare the immutable base revision and atomically write one new draft revision with bounded undo. Discard and Undo SHALL be free and idempotent. A stale proposal SHALL preserve manual edits and require explicit re-quote. Desktop docked chat and mobile step SHALL expose identical labelled diffs and recovery actions.

#### Scenario: Stale or duplicate Apply
- **WHEN** a proposal is applied twice or the draft has been manually changed
- **THEN** a duplicate returns its recorded result and a stale proposal refuses to overwrite the draft

### Requirement: Private recoverable proposal lifecycle
The API SHALL allowlist and catalogue proposal lifecycle events before emission, honor opt-out and emit identifiers and reason codes only. Proposal data SHALL export and erase with its learner, expire with its brief within 30 days and join encrypted nightly 14-day backups with erasure replay. Financial records SHALL retain their separate existing policy. GET reads SHALL never settle or fail proposals. A periodic worker reaper SHALL release expired pending proposals and recover validated delivery. All state transitions SHALL compare the expected state and check affected rows; settlement and delivery/failure SHALL commit atomically so stale workers and reapers cannot overwrite each other. Offline mock-provider golden cases SHALL gate deterministic diff validity, manual edit preservation, injection, insufficient balance and failed reservation release.

#### Scenario: Restore and erase
- **WHEN** a draft and proposal are restored or the learner is deleted
- **THEN** restored proposals require no provider work and deletion removes private proposal content through the existing erasure policy
