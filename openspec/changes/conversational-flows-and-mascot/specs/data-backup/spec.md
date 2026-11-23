## ADDED Requirements

### Requirement: Guided chat drafts and mascot preference in backup and restore
Postgres table `guided_chat_drafts` and the `mascot` object in Learner settings SHALL be covered by encrypted nightly dumps (14 days). The restore drill MUST restore both, then apply expiry purge and erasure tombstones before serving. Drafts MUST expire after 30 days idle and MUST cascade on account erasure. Secrets, codes and destructive confirmations MUST never be persisted.

#### Scenario: Restore drill
- **WHEN** the drill restores a dump containing an expired draft and an erased account
- **THEN** the expired draft and the erased account's rows are absent after restore completes
