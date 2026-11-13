## ADDED Requirements

### Requirement: Publication draft data
`publication_drafts` (snapshots and version refs) SHALL be in nightly dumps and the versioned bucket, and `publication_screenings` rows (verdict, cost, codes only) SHALL be retained one year. Neither holds private learner payload. Account deletion MUST remove the owner's drafts and screenings and the export MUST list drafts by number, date and verdict. The restore check MUST cover both tables.

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** no draft or screening row references them
