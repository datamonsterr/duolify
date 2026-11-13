## ADDED Requirements

### Requirement: Sharing data backup
`courses.visibility`, `publication_links` (hash only, never token plaintext), course draft references MUST be included in nightly dumps. Snapshot assets are covered by existing object storage protection. docs/data-inventory.md MUST be updated.

#### Scenario: Nightly dump
- **WHEN** the nightly backup runs
- **THEN** `courses.course_visibility` and `publication_links` rows (token_hash, created_at, revoked_at, course_id) are included; plaintext tokens MUST NOT appear in any backup log or dump

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** all `publication_links` rows for their Courses are removed and no token hash referencing them remains in the dump
