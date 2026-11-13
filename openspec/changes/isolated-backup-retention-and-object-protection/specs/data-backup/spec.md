## MODIFIED Requirements

### Requirement: Retention
General dumps MUST be kept for 14 days and financial dumps for 35 days. Expiry
MUST run as a job under an identity separate from the append-only backup writer,
and MUST NOT delete the account-deletion ledger or shorten live seven-year
financial history. Railway volume snapshots and PITR remain a second, owner-provided
layer. Backups MUST live in a different encrypted bucket from application data, with no application delete rights.

#### Scenario: Old dumps
- **WHEN** a general dump is older than 14 days or a financial dump older than 35 days
- **THEN** the separate expiry job removes it and keeps newer dumps and the deletion ledger

#### Scenario: Writer cannot delete
- **WHEN** the backup writer or application identity attempts to delete a dump
- **THEN** storage denies the request

### Requirement: Object storage protection
Object storage buckets for Slides, assets and mirrored Golden set revisions MUST have versioning enabled. The application identity MUST NOT delete versions or change versioning. Pinned golden set dataset revisions mirrored to S3 MUST be verified with SHA-256 checksums. Slides MUST be regenerable only through a tracked job, never silently. Recovery and regeneration SHALL append a new version under the same key tagged with an operator job id.

#### Scenario: Object overwritten
- **WHEN** a Slide object is overwritten by mistake
- **THEN** an operator job restores the previous version under the same key and the same reference serves it

#### Scenario: Untracked regeneration
- **WHEN** regeneration is requested without a job id
- **THEN** it is refused and nothing is written

### Requirement: Retention and recovery reporting
Failed retention or recovery jobs SHALL emit `backup_failed` with the reserved operations UUID and common properties, honor global opt-out, and never include keys, errors or content.

#### Scenario: Expiry fails
- **WHEN** the expiry job cannot list or delete
- **THEN** `backup_failed` is delivered without object keys
