# analytics Specification

## Purpose
Define how Duolify uses PostHog: identity, privacy, the event catalog with names and properties, and LLM analytics. Feature specs refer to this catalog.

## Requirements

### Requirement: Identity and common properties
Events SHALL use the internal Learner id (UUID) as `distinct_id`, never email. Every event MUST include `platform` (`desktop`, `ios`, `android`, `web`), `app_version` and `source` (`client` or `api`). Events about a Course or Lesson MUST include `course_id` and `lesson_id` when known.

#### Scenario: Event without email
- **WHEN** any event is sent
- **THEN** `distinct_id` is the Learner UUID and no email or free text is in properties

### Requirement: Privacy rules
Events MUST NOT contain note text, chat content, profile text, Slide HTML or answers. A Learner opt-out MUST stop all events. Server-side events SHALL be sent by the API through a non-blocking queue so analytics failure never breaks a request.

#### Scenario: PostHog down
- **WHEN** PostHog is unreachable
- **THEN** the API request still succeeds and events are dropped or retried in the background

### Requirement: Course and lesson events
The catalog MUST include: `course_preview_requested` (topic_length, has_profile), `course_preview_viewed` (course preview id, duration_ms), `course_outline_generated` (course_id, unit_count, lesson_count, provider, model, latency_ms), `course_enrolled` (course_id), `course_generation_failed` (stage, error_code), `lesson_generation_requested` (course_id, lesson_id), `lesson_generated` (lesson_id, slide_count, provider, model, latency_ms), `lesson_started`, `slide_viewed` (index, watch_ms, offline), `slide_replayed`, `slide_paused`, `lesson_completed` (xp, duration_ms), `lesson_player_error` (error_code, slide_id), `lesson_test_started`, `lesson_test_submitted`, `lesson_test_passed` and `lesson_test_failed` (score, attempt, duration_ms).

#### Scenario: Catalog check
- **WHEN** a feature emits an event
- **THEN** its name and properties appear in the catalog or the change adds them

### Requirement: Gamification and social events
The catalog MUST include: `xp_earned` (amount, source), `daily_goal_met` (goal_xp), `streak_extended` (streak_length), `streak_broken` (previous_length), `streak_freeze_earned` (reason), `streak_freeze_used` (remaining), `league_promoted` and `league_demoted` (league, rank), `friend_request_sent` (method), `friend_request_accepted`, `friend_streak_started`, `friend_streak_extended` (length), `friend_streak_broken` (length) and `friend_nudge_sent`.

#### Scenario: Streak broken
- **WHEN** a Streak resets
- **THEN** `streak_broken` carries `previous_length`

### Requirement: Credits and AI feature events
The catalog MUST include: `credits_reserved`, `credits_settled`, `credits_released` (feature, amount), `credits_insufficient` (feature, balance, estimate), `credits_purchased` (amount, currency, price), `credits_purchase_failed` (reason), `reward_credits_granted` (reason, amount), `tutor_message_sent` (has_lesson_context), `tutor_reply_completed`, `tutor_reply_stopped`, `tutor_reply_failed` (error_code), `quick_note_created` (source, length), `quick_note_enriched`, `quick_note_enrichment_failed`, `quick_note_reviewed`, `quick_note_deleted`, `bookmark_added`, `bookmark_removed`, `bookmark_opened` (target_type), `learner_profile_updated` (fields_changed) and `onboarding_completed` (no domain properties).

#### Scenario: Purchase event
- **WHEN** a webhook confirms a purchase
- **THEN** `credits_purchased` is emitted once with `amount`, `currency` and `price`

### Requirement: Pricing exposure events
The catalogue SHALL include `pricing_version_exposed` with `pricing_version` and `surface` (`paywall`, `quote`, `pack_list`, `plan_list`). The client SHALL emit at most once per authenticated Learner session, surface and version through the existing analytics wrapper, with UUID identity, platform, app_version and source. Opt-out SHALL suppress delivery; no learner text, provider mapping or secrets may be included. Signed-out catalogue browsing SHALL not fabricate a Learner identity.

#### Scenario: Repeated price view
- **WHEN** a signed-in Learner opens the same price surface twice in one session
- **THEN** one exposure event reaches PostHog, unless analytics is disabled

### Requirement: Pricing lifecycle events
The catalogue SHALL include `pricing_version_created`, `pricing_version_approved`, `pricing_version_rejected`, `pricing_version_activated`, `pricing_version_rolled_back` and `price_table_activated` with `pricing_version`, `change_class` (`adverse` or `non_adverse`), and `actor_id`. The API SHALL emit them through the existing UUID/common-properties wrapper. Events MUST NOT include reasons, full pricing content, credentials or provider mappings. Server opt-out SHALL suppress delivery. Activation events SHALL be recorded once per version in Postgres before delivery.

#### Scenario: Pricing lifecycle
- **WHEN** a pricing administrator creates, approves or rejects a version
- **THEN** the corresponding lifecycle event is emitted with common properties and no private reason text

#### Scenario: Scheduled activation
- **WHEN** database time reaches an approved version's effective instant
- **THEN** activation is durably recorded once and both activation events are emitted through the existing wrapper without private pricing data

### Requirement: LLM analytics
Every Engine call SHALL produce a PostHog `$ai_generation` event from the API with `$ai_provider`, `$ai_model`, `$ai_input_tokens`, `$ai_output_tokens`, `$ai_latency`, `feature`, `fallback_used` and `$ai_is_error`. Prompt and completion text MUST NOT be captured.

#### Scenario: Fallback call
- **WHEN** the Engine used the fallback provider
- **THEN** `$ai_generation` has `fallback_used` true and the real provider and model

### Requirement: Operations events
The API SHALL emit `backup_completed` and `backup_failed` (size_bytes, duration_ms, ok), `restore_drill_completed` (duration_ms, ok) and `eval_run_completed` (task, score, threshold, passed) from workers and CI, with `source` set to `api` or `ci`. Operational jobs without a Learner SHALL use the reserved system UUID `00000000-0000-4000-8000-000000000011`, platform `web`, and app_version from build information (`development` for local builds). This UUID never identifies a Learner. `ANALYTICS_OPT_OUT=true` SHALL suppress server analytics globally, including operations events. No database contents, object keys, credentials, provider errors or free text SHALL be sent by backup emitters.

#### Scenario: Backup job done
- **WHEN** the nightly dump completes
- **THEN** `backup_completed` is emitted with size and duration

### Requirement: Event testing and change control
Each event MUST have a unit test for its emitter and the e2e suite MUST check the key funnel events with a PostHog test double. Renaming or removing an event MUST be done through an OpenSpec change that updates this catalog.

#### Scenario: New event without catalog entry
- **WHEN** a pull request adds an event name not in the catalog
- **THEN** the analytics test fails

### Requirement: Analytics data backup and retention
PostHog is a derived store and MUST NOT be the only copy of any product state. Event retention in PostHog SHALL follow the project plan, and the event catalog in this spec MUST be the source of truth. Deleting an account MUST also request deletion of the Learner's PostHog person.

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** a PostHog person deletion request is made

#### Scenario: Authoritative quote terms exposed
- **WHEN** an action quote is displayed or renewed
- **THEN** `pricing_version_exposed` uses surface `quote` and the quote's original version, deduplicated by authenticated session, surface and version; common UUID/platform/app_version/source properties are present and opt-out prevents delivery
- **AND** action input, learner text, model mappings and quote payloads are excluded
