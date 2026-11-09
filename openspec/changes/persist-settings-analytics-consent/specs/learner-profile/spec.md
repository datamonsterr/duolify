## ADDED Requirements

### Requirement: Persisted learner settings
The learner profile SHALL persist appearance, motion, narration, narration speed, captions, text size, UI sounds and haptics alongside existing locale, time zone, daily goal and reminder preferences. Settings changes SHALL survive application restart. The profile API SHALL validate enum/range values and preserve fields omitted from a partial update.

#### Scenario: Settings survive restart
- **WHEN** a learner saves settings and returns in a new app session
- **THEN** the API returns the saved values and the controls show them

#### Scenario: Invalid setting
- **WHEN** a client submits an unsupported appearance, speed or text size
- **THEN** the API rejects the update without changing any persisted preference

### Requirement: Confirmed setup preferences
Daily-time/XP-goal and reminder preferences collected during setup SHALL be persisted only when the learner confirms setup. Draft chat answers SHALL remain unsaved until explicit confirmation.

#### Scenario: Setup draft
- **WHEN** the learner leaves setup before confirming its summary
- **THEN** the newly entered preferences are not persisted

### Requirement: Settings analytics and backup
Preference changes SHALL emit only the catalogued consent and appearance events with UUID/common properties, and no profile text or other private values. Preference and consent fields SHALL remain in the current `learner_profiles` row, be included in nightly Postgres dumps, and cascade-delete with the Learner; PostHog SHALL remain derived.

#### Scenario: Restore settings
- **WHEN** a nightly dump is restored
- **THEN** all persisted settings and analytics consent values match the source profile
