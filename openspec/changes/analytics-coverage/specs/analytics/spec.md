## MODIFIED Requirements

### Requirement: Identity and common properties
Events SHALL use the internal Learner id (UUID) as `distinct_id`, never email. Every event MUST include `platform` (`desktop`, `ios`, `android`, `web`), `app_version` and `source` (`client`, `api` or `engine`). Events about a Course or Lesson MUST include `course_id` and `lesson_id` when known.

#### Scenario: Event without email
- **WHEN** any event is sent
- **THEN** `distinct_id` is the Learner UUID and no email or free text is in properties

### Requirement: Credits and AI feature events
The financial purchase catalogue SHALL include `checkout_started` (sku_id, channel,
currency, amount_minor, price_table_version), `credits_purchased` (amount, currency,
price, sku_id, channel, amount_minor, price_table_version), `credits_refunded` (amount, payment_reference, reason_code), and
`credits_purchase_failed` (reason, sku_id, channel, price_table_version).
These API events SHALL use transactionally unique outbox keys, UUID/common properties
and opt-out. No raw payment payloads, URLs, learner text or provider errors are allowed.

The catalog MUST include: `credits_reserved`, `credits_settled`, `credits_released` (feature, amount), `credits_insufficient` (feature, balance, estimate), `credits_purchased` (amount, currency, price), `credits_purchase_failed` (reason), `credits_refunded` (amount, payment_reference, reason_code), `daily_credits_granted` (amount), `daily_credits_expired` (amount), `tutor_message_sent` (has_lesson_context), `tutor_reply_completed`, `tutor_reply_stopped`, `tutor_reply_failed` (error_code), `quick_note_created` (entry_point, length), `quick_note_enriched`, `quick_note_enrichment_failed`, `quick_note_reviewed`, `quick_note_deleted`, `bookmark_added`, `bookmark_removed`, `bookmark_opened` (target_type), `learner_profile_updated` (fields_changed) and `onboarding_completed` (no domain properties). The catalog MUST NOT contain `reward_credits_granted`.

#### Scenario: Purchase event
- **WHEN** a webhook confirms a purchase
- **THEN** `credits_purchased` is emitted once with `amount`, `currency` and `price`

### Requirement: Operations events
The API SHALL emit `backup_completed` and `backup_failed` (size_bytes, duration_ms, ok), `restore_drill_completed` (duration_ms, ok) and `eval_run_completed` (task, score, threshold, passed, dataset_revision, dataset_source, provider, status, budget), and `ai_cost_budget_alert` (scope, actual_usd, budget_usd) from workers, API, and CI, with `source` set to `api` or `engine` according to the emitting tier. Operational jobs without a Learner SHALL use the reserved system UUID `00000000-0000-4000-8000-000000000011`, platform `web`, and app_version from build information (`development` for local builds). This UUID never identifies a Learner. `ANALYTICS_OPT_OUT=true` SHALL suppress server analytics globally, including operations events. No database contents, object keys, credentials, provider errors, prompts, or free text SHALL be sent by operations emitters.

#### Scenario: Backup job done
- **WHEN** the nightly dump completes
- **THEN** `backup_completed` is emitted with size and duration

#### Scenario: Eval run reported
- **WHEN** an evaluation task completes
- **THEN** `eval_run_completed` is emitted with task, score, threshold, passed status, dataset revision, and dataset source metadata

#### Scenario: Cost budget alert fired
- **WHEN** spend reaches 80% or 100% of the platform daily budget
- **THEN** `ai_cost_budget_alert` is emitted with `scope`, `actual_usd`, and `budget_usd` under the system operational UUID without learner content

#### Scenario: Analytics opted out during alert
- **WHEN** server analytics is opted out or unavailable
- **THEN** server alert counters and operational enforcement continue without failing requests

### Requirement: Quick notes and sheets event catalogue
The analytics catalogue SHALL include `quick_note_created` (carrying `entry_point` restricted to `slide|lesson|chat`, and integer `length`), `ui_sheet_opened` (carrying `sheet_id`), `quick_note_reviewed` (carrying `note_id`), `quick_note_deleted` (carrying `note_id`), and `ui_control_activated` with `control_id=export_notes`. All events SHALL use Learner UUID distinct IDs and common properties (`platform`, `app_version`, `source`), and respect analytics opt-out. Note text, search queries, draft content, slide HTML, and private learner data MUST NOT enter PostHog analytics under any circumstance.

#### Scenario: Quick note created event
- **WHEN** a learner captures a quick note from a slide, lesson, or chat
- **THEN** `quick_note_created` is emitted with the verified source and character length, excluding note text

#### Scenario: Quick note sheet opened
- **WHEN** a learner opens a quick note sheet or panel
- **THEN** `ui_sheet_opened` is emitted with `sheet_id=quick_note` and common properties, excluding note text

#### Scenario: Quick note reviewed event
- **WHEN** a learner marks a note as reviewed
- **THEN** `quick_note_reviewed` is emitted with the note UUID and common properties, excluding note text

#### Scenario: Quick note deleted event
- **WHEN** a learner deletes a quick note
- **THEN** `quick_note_deleted` is emitted with the note UUID and common properties, excluding note text

#### Scenario: Export notes control activated
- **WHEN** a learner triggers note export
- **THEN** `ui_control_activated` is emitted with `control_id=export_notes` and surface context, excluding note content

### Requirement: Save and delete thread events
`quick_note_created` SHALL carry `entry_point=chat` and `length` for a note saved from a reply, once per note (not on a replayed save). The client SHALL emit `ui_control_activated` with `surface=tutor_chat` and `control_id=delete_thread` when the Learner confirms deletion. Events MUST carry the common properties and Learner UUID, honour opt-out and never include message, note or thread text.

#### Scenario: Opted out
- **WHEN** the Learner has opted out
- **THEN** neither event is delivered

### Requirement: Enrollment analytics events
The API SHALL emit `course_enrolled_published` (course_id: UUID of the copy, source_course_id: UUID, entry_point: `link`, version: int) once when a copy is created, and `published_lesson_reused` (course_id: UUID, lesson_id: UUID, version: int) when a reused Lesson becomes available. Events MUST NOT contain tokens, usernames, titles, Slide content or answers, and MUST respect analytics opt-out.

#### Scenario: Enroll event once
- **WHEN** a Learner enrolls twice through a link
- **THEN** `course_enrolled_published` is emitted once with entry_point `link`

### Requirement: Course invite analytics events
The API SHALL emit `course_invite_sent` (method `username` or `friend`, course_id, invite_id), `course_invite_accepted` and `course_invite_declined` (course_id, invite_id), and `course_enrolled_published` with entry_point `invite`. Properties MUST be UUIDs and enums, never Usernames or titles, and opt-out MUST be honored. The catalogue MUST list the events before an emitter exists.

#### Scenario: Accept emits enrollment
- **WHEN** a Learner accepts an invite
- **THEN** `course_invite_accepted` and `course_enrolled_published` with entry_point `invite` are captured and nothing is sent for an opted-out Learner

### Requirement: Welcome and tour funnel catalogue
The client SHALL emit onboarding_welcome_shown, onboarding_path_chosen with path topic/setup_chat/example/skip, onboarding_tour_started with entry_point first_run/settings, onboarding_tour_step_viewed with step path/next_lesson/credits/progress/notes/courses/settings, onboarding_tour_completed and onboarding_tour_skipped with at_step from the same step enum. The existing consent-aware wrapper SHALL enforce learner UUID and common platform/app_version/source properties, and reject unknown enums. No free text SHALL be captured. Existing sign-up and lesson events SHALL remain joinable by learner UUID.

#### Scenario: Consent disabled
- **WHEN** an opted-out learner uses welcome or tour
- **THEN** no onboarding events are captured

### Requirement: Check-in grant analytics
The API MUST enqueue `check_in_credits_granted` atomically with each new attendance check-in, including a zero amount at the bank cap, idempotently, with only closed non-PII `amount`, `first_time`, `banked_after` and `capped` properties plus canonical common properties. Delivery MUST respect existing analytics opt-out and MUST NOT affect grant correctness. No toast event is required. Event properties MUST follow existing outbox retention and backup rules.

#### Scenario: Consent and replay
- **WHEN** an opted-out Learner checks in or a committed request is retried
- **THEN** Credits are correct and no opted-out or duplicate event is delivered
