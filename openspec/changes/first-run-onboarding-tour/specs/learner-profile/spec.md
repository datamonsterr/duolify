## ADDED Requirements

### Requirement: First-run learning welcome
The Learn home SHALL offer new learners a skippable welcome with topic entry into the existing course brief, Help me decide into existing setup chat, and contract-backed example enrollment when available. The welcome SHALL NOT duplicate learner context questions or interrupt deep links, lessons, invites or the SSO bridge. Returning learners existing at migration SHALL be exempt.

#### Scenario: Topic handoff
- **WHEN** a new learner enters a topic
- **THEN** the existing course brief opens with the topic prefilled and editable, without starting a paid action

#### Scenario: Example feature absent
- **WHEN** GET /v1/courses/examples returns 404 or no examples
- **THEN** the example path is hidden and other paths and Skip remain available

### Requirement: Per-learner onboarding progress
The authenticated API SHALL read and partially update independent welcome and tour pending/skipped/completed enums. Completed SHALL win concurrent skipped writes and no terminal state SHALL reset to pending. Progress SHALL survive other devices and reinstall; local learner-scoped fallback SHALL queue offline writes. Replay SHALL NOT reset progress, streaks or other preferences.

#### Scenario: Another device
- **WHEN** a learner completes or skips onboarding and signs in on another device
- **THEN** the server state suppresses automatic welcome and tour

### Requirement: Onboarding data lifecycle and reliability
Onboarding enums SHALL be exported with learner profiles, cascade-erased with the account, included in 14-day nightly encrypted backups and classified in the backup anonymizer. Existing model evaluations, fallback gates and setup completion semantics SHALL be preserved; no new AI behavior SHALL be introduced.

#### Scenario: Account erasure
- **WHEN** an account is erased or its tombstone replayed after restore
- **THEN** its onboarding progress is erased with the profile
