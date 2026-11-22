## MODIFIED Requirements

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

### Requirement: Publication draft data
`publication_drafts` (snapshots and version refs) SHALL be in nightly dumps and the protected asset copy under `Object storage protection`, and `publication_screenings` rows (verdict, cost, codes only) SHALL be retained one year. Neither holds private learner payload. Account deletion MUST remove the owner's drafts and screenings and the export MUST list drafts by number, date and verdict. The restore check MUST cover both tables.

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** no draft or screening row references them

### Requirement: Copy-local generated Lessons are backed up
Lessons generated in a Course copy and their consent records SHALL be included in the nightly encrypted Postgres dump and the protected asset copy under `Object storage protection`. Restore MUST return them with the copy and the immutable published hash; account deletion MUST erase them.

#### Scenario: Restore keeps copy-local content
- **WHEN** a dump with a generated copy Lesson is restored
- **THEN** the Lesson and its consent record exist and the pin hash is unchanged
