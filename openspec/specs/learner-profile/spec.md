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
A Learner SHALL be able to set a level per Course that overrides the global level for that Course, and reset it to revert to the global profile level. Setting or resetting a per-course level MUST NOT alter or overwrite the Learner's global profile level stored in `learner_profiles`. The API context builder SHALL project an effective profile combining the global profile with the Course's level override for AI engine requests.

#### Scenario: Course override
- **WHEN** a Course level is set
- **THEN** AI requests for that Course use the Course level
- **AND** the global profile level remains unchanged

#### Scenario: Course level reset
- **WHEN** a Course level override is reset
- **THEN** AI requests for that Course fall back to the global profile level
- **AND** the global profile level remains unchanged

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

### Requirement: Guided conversational setup chat
The API SHALL expose an authenticated `POST /v1/me/setup-chat` endpoint backed by the stateless Engine `SetupChat` RPC. The chat SHALL guide the Learner through onboarding questions covering goal, topic, known background/level, daily time/XP goal, reminders, and detected confirmable time zone. Every question MUST be skippable. Only a vague answer SHALL trigger one clarifying follow-up question per step; subsequent answers for that step MUST NOT trigger a second follow-up. An editable live summary SHALL update as the Learner answers. No profile write SHALL occur before explicit confirmation. If the assistant fails, times out, or is cancelled, collected answers MUST be kept intact and the equivalent setup form MUST be opened. The setup chat SHALL also be accessible from the Profile screen with existing values prefilled.

#### Scenario: Vague goal answer triggers single follow-up
- **WHEN** the Learner answers a vague goal such as "something about space"
- **THEN** the assistant asks one clarifying follow-up question with quick replies and extracts the tentative topic

#### Scenario: Second answer does not trigger further follow-up
- **WHEN** the Learner provides an answer to the clarifying follow-up question
- **THEN** the assistant proceeds to the next setup step without asking another follow-up

#### Scenario: Skipped question preserves existing field
- **WHEN** the Learner selects Skip for an onboarding question
- **THEN** that field remains unchanged or empty, no value is invented, and the assistant advances to the next step

#### Scenario: Assistant failure falls back to form with answers intact
- **WHEN** the setup chat encounters a provider error or timeout
- **THEN** the client displays the form fallback keeping all collected answers editable without loss

### Requirement: Setup chat analytics events
The system SHALL emit PostHog analytics events during guided setup chat. The client SHALL emit `ui_control_activated` with surface `guided_chat` and control ID in `quick_reply|skip|form_fallback|confirm_summary`. The API SHALL emit `$ai_generation` for each assistant completion (recording model, provider, tokens, latency, and fallback status, but NEVER recording Learner free text or prompt content). Canonical profile save upon confirmation SHALL emit `learner_profile_updated` (with `fields_changed` names only) and `onboarding_completed` exactly as specified in E1-03. An opted-out Learner MUST NOT produce any PostHog events.

#### Scenario: Control activation analytics
- **WHEN** a Learner taps a quick-reply button or skips a question
- **THEN** `ui_control_activated` is emitted with the corresponding control ID and no private answer text

#### Scenario: Privacy and opt-out
- **WHEN** an opted-out Learner participates in setup chat
- **THEN** neither UI nor AI generation events are sent to PostHog

### Requirement: Setup chat data backup and retention
Unconfirmed setup chat transcripts and live summaries SHALL remain ephemeral session drafts in device memory and MUST NOT be persisted on the server, in the database, in the Engine, or in analytics. Only the confirmed profile/preferences SHALL be persisted to Postgres `learner_profiles` through the canonical E1-03 endpoint, covered by 14-day nightly encrypted backups, export, and account deletion cascades within 30 days.

#### Scenario: Abandoned unconfirmed setup chat
- **WHEN** a Learner navigates away from or closes unconfirmed setup chat
- **THEN** no unconfirmed profile data or transcript is persisted on the server

### Requirement: Setup chat AI reliability and evals
The setup chat feature SHALL include golden-set cases in `evals/fixtures/` covering vague versus specific answers, the one-follow-up bound, skipped answers, non-English answers, prompt injection resistance, summary extraction fidelity, no profile save before confirmation, and assistant failure form recovery. These cases MUST pass `mise run engine:eval` against the required threshold. The Engine MUST enforce token limits, schema validation, and config timeouts below API deadlines. In case of provider failure, the Engine router MUST attempt one fallback before returning a typed error that triggers form fallback.

#### Scenario: Evaluation against golden set
- **WHEN** `engine:eval` runs in offline mock mode
- **THEN** all `setup_chat` cases pass deterministic checks and meet the minimum score threshold

### Requirement: Bounded setup chat draft fields
Setup chat SHALL reject draft text exceeding the configured profile text limit (2000 Unicode characters by default) and code fields exceeding 64 characters, with reminder time limited to five characters. Invalid drafts SHALL be rejected before engine invocation or persistence. This validation SHALL add no analytics events or learner text to analytics, and SHALL leave AI fallback and eval behavior unchanged.

#### Scenario: Oversized unconfirmed draft
- **WHEN** an authenticated caller submits an oversized draft field
- **THEN** the API returns a bad request without calling the setup chat engine or saving the draft
