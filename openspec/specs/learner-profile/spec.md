# learner-profile Specification

## Purpose
Store the Learner's background, goals and level so AI features can personalize Courses, Lessons and Tutor chat. The Learner controls this data.

## Requirements

### Requirement: Profile fields
A Learner SHALL provide optional background, goals, level, locale and time zone. Time zone and locale MUST default from the device. Free text fields MUST have a length limit in config.

#### Scenario: First sign-in
- **WHEN** a new Learner signs in
- **THEN** a profile exists with device locale and time zone and empty background

#### Scenario: Text too long
- **WHEN** background text exceeds the limit
- **THEN** the API rejects it with a validation error

### Requirement: Use as AI context
The API SHALL send the profile as `LearnerProfile` in Engine requests for preview, outline, Lesson generation, chat and note enrichment. Fields the Learner left empty MUST be sent empty. The Engine MUST NOT persist the profile.

#### Scenario: Profile edited
- **WHEN** a Learner updates their level
- **THEN** the next Engine request carries the new level

### Requirement: Per-course level
A Learner SHALL be able to set a level per Course that overrides the global level for that Course.

#### Scenario: Course override
- **WHEN** a Course level is set
- **THEN** AI requests for that Course use the Course level

### Requirement: Learner control and privacy
A Learner MUST be able to view, edit, export and delete profile data. Profile text MUST be sent only to the configured AI providers and MUST NOT be sent to PostHog. The client SHALL disclose this use before the Learner enters background text.

#### Scenario: Delete background
- **WHEN** a Learner clears the background
- **THEN** later AI requests contain no background

### Requirement: Profile analytics events
The system SHALL emit `learner_profile_updated` with `fields_changed` (names only, never values), `onboarding_completed` and `analytics_opt_out_changed`. A Learner who opts out MUST NOT produce further PostHog events.

#### Scenario: Opt out
- **WHEN** a Learner turns off analytics
- **THEN** the client and API stop sending events for that Learner

### Requirement: Profile data backup and retention
Profile rows MUST be stored in Postgres, covered by the nightly dump, and deleted with the account within 30 days. Profile history is not kept; only the current values are stored.

#### Scenario: Account deletion
- **WHEN** a Learner deletes their account
- **THEN** the profile row is removed within 30 days and backups roll off by the 14-day retention

### Requirement: Personalization testing
Golden set cases for Course and Lesson generation and Tutor chat MUST include varied profiles (beginner, expert, non-English locale, empty profile). Deterministic checks MUST verify that the output language matches locale and that empty profiles do not produce invented background.

#### Scenario: Empty profile
- **WHEN** a Golden set case has an empty profile
- **THEN** the output makes no claim about the Learner's background
