## ADDED Requirements

### Requirement: Explicit optional context confirmation
The API SHALL expose authenticated GET and POST /v1/me/profile and GET /v1/me/profile/export. POST MUST require confirmed=true and action save or skip. Only Save/Confirm or Skip SHALL complete setup; draft edits and reads MUST NOT write. Save SHALL accept partial optional fields; omitted fields MUST retain current values, empty strings and null numeric/boolean fields MUST clear values. Skip SHALL preserve persisted context and discard collected answers. Locale and IANA time zone SHALL start with device values established at sign-in, and clearing them SHALL remain empty on subsequent reads.

#### Scenario: Confirmation and clear
- **WHEN** a learner confirms an edited goal and clears background
- **THEN** only those fields change, background reads empty, and analytics and age preferences remain unchanged

#### Scenario: Skip draft
- **WHEN** a learner skips setup with unsaved answers
- **THEN** setup completes without persisting those answers

### Requirement: Profile validation and form parity
The API SHALL bound goal, topic and background by configurable Unicode character limits (default 2000), level by empty/beginner/intermediate/advanced/expert, daily minutes by 1–1440 or null, daily XP by 1–10000 or null, reminder enabled by boolean or null, and reminder time by empty or HH:MM. Nonempty locale MUST be a language tag and nonempty time zone MUST be IANA or UTC. The response SHALL include the configured text limit. The client SHALL disclose configured AI-provider use before entry, retain drafts on error, show field errors and loading/offline recovery, and offer all fields in an always-reachable form. A controlled initialAnswers handoff SHALL carry every collected chat answer into the same editable summary/form draft; chat orchestration remains E1-11. Desktop panels SHALL become mobile steps with identical edit/confirm/skip/clear/export actions, using design tokens, keyboard controls, light/dark and reduced motion.

#### Scenario: Form handoff
- **WHEN** chat hands collected answers to the form
- **THEN** every answer is editable and no write happens until explicit confirmation

#### Scenario: Invalid or unavailable save
- **WHEN** validation or the network rejects a save
- **THEN** the draft is retained with a labelled error and retry action

### Requirement: Confirmed profile analytics catalogue
The API SHALL own learner_profile_updated (fields_changed: allowlisted names only) and onboarding_completed (no domain properties), sent only after commit with UUID distinct_id and platform, app_version and source common properties through the existing PostHog wrapper. The first completed save or skip SHALL emit onboarding_completed exactly once. Effective changed context fields SHALL emit learner_profile_updated; empty changes SHALL not emit it. Opted-out learners MUST produce neither event. Learner text MUST NEVER enter analytics. Client MUST NOT duplicate these domain events.

#### Scenario: Opted out confirmation
- **WHEN** an opted-out learner saves or skips
- **THEN** no PostHog event is delivered

### Requirement: Current context recovery and reliability
Only current profile values SHALL be stored in Postgres, included in nightly encrypted dumps, exportable and cascade-erased with learner deletion within 30 days; general backups SHALL roll off after 14 days. Restore and deletion checks MUST cover every new persisted field and the data inventory MUST document them. This change SHALL add no AI behavior and SHALL preserve existing evaluation and fallback gates.

#### Scenario: Restore then delete
- **WHEN** a backup is restored and its synthetic learner is deleted
- **THEN** all context and completion values restore faithfully and the profile row cascades away
