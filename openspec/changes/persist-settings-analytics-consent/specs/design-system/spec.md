## ADDED Requirements

### Requirement: Settings screen
Settings SHALL provide appearance, motion, narration, narration speed, captions, text size, UI sounds, haptics, locale/time zone, daily XP goal, reminders and analytics consent controls. It SHALL show loading, saved and recoverable error states. Desktop sections SHALL become stacked mobile sections with the same actions. Profile SHALL retain its form editor and Update by chat entry.

#### Scenario: Save settings
- **WHEN** a learner changes settings and saves
- **THEN** the saved values remain visible and survive reload

#### Scenario: Save failure
- **WHEN** a settings update fails
- **THEN** the last saved values remain identifiable, the draft is retained, and the learner can retry

### Requirement: Settings analytics events
The catalogue SHALL include `ui_appearance_changed` with `mode` limited to system, light or dark. Events SHALL use only stable enum values and UUID/common properties.

#### Scenario: Appearance change
- **WHEN** an opted-in learner saves a changed appearance
- **THEN** one `ui_appearance_changed` event is delivered after persistence

### Requirement: Settings event privacy and retention
Settings events SHALL contain only the stable appearance enum, UUID and common properties. Preference values SHALL be backed up in Postgres with the learner profile and SHALL be removed on account deletion; PostHog SHALL remain derived.

#### Scenario: Settings restore
- **WHEN** the profile is restored from the nightly dump
- **THEN** saved settings remain available and consent remains unchanged
