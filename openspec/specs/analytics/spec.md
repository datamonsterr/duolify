# analytics Specification

## Purpose
Define how Duolify uses PostHog: identity, privacy, the event catalog with names and properties, and LLM analytics. Feature specs refer to this catalog.
## Requirements
### Requirement: Published Character snapshot analytics
The API SHALL emit `character_snapshot_published` with integer `course_version`,
`character_count` and `custom_count` once per successfully published Course version.
It SHALL include Learner UUID and common properties, respect current opt-out and
use the non-blocking PostHog adapter. No Character text, prompts, persona, dialogue,
answers, specs, SVG or private ownership links may be included. Persisted publication
state, rather than analytics, SHALL deduplicate events and recover content.

#### Scenario: Publication retries
- **WHEN** the same version is published or screened again
- **THEN** at most one snapshot publication event is delivered and no Character content is captured

### Requirement: Shared demo analytics catalogue
The existing client event `course_preview_viewed` SHALL allow preview_kind=shared_demo and demo_version (fixed authored version), without a financial preview_id. The existing `ui_control_activated` SHALL allow control_id=shared_demo with surface=welcome|course_brief. Both SHALL carry UUID/common properties and honor opt-out. No paid generation, model usage, text, topic, title, HTML or draft properties SHALL be emitted for the shared demo.

#### Scenario: Inspect shared authored content
- **WHEN** a Learner opens the shared demo
- **THEN** the client emits the control and viewed events with content-free properties and no paid event

### Requirement: Playback control telemetry
The catalogue SHALL include slide_viewed with lesson_id, slide_id, zero-based index, measured playing wall-clock watch_ms and offline; slide_replayed and slide_paused with lesson_id, slide_id and index; and lesson_player_error with fixed error codes including position_unavailable. Events SHALL use authenticated UUID/common properties and honor opt-out. The wrapper SHALL discard text, HTML, URLs and unregistered properties. Only ended SHALL emit slide_viewed, once per attempt; playback SHALL never emit lesson_completed.

#### Scenario: End and replay
- **WHEN** a slide ends and duplicate ended messages arrive before Replay
- **THEN** one slide_viewed emits and replay begins a new attempt with slide_replayed

### Requirement: Sample outline proposal lifecycle
The catalogue SHALL include API-owned `outline_proposal_requested`, `outline_proposal_delivered`, `outline_proposal_failed`, `outline_proposal_applied`, `outline_proposal_discarded`, and `outline_proposal_undone`, carrying only proposal_id and base_revision plus UUID/common properties. The existing client `ui_control_activated` SHALL allow surface=outline and control_id=outline_change_requested|proposal_apply|proposal_discard|outline_undo. All events honor current opt-out and exclude request text, titles, goals and diffs. Canonical credits_reserved/settled/released and $ai_generation remain owned by the financial outbox.

#### Scenario: Private delivered diff
- **WHEN** a valid proposal is delivered and later discarded
- **THEN** lifecycle events contain only opaque identity and revision properties, with no second usage or charge event

### Requirement: Manual outline controls
The catalogue SHALL include existing `ui_control_activated` with `surface=outline` and `control_id=outline_move|outline_rename|outline_remove|outline_add|outline_undo`. Move includes drag, arrow keys and move buttons. The API allowlist SHALL retain this event and its surface/control properties. Client events SHALL include Learner UUID, platform, source and app_version, honor opt-out and contain no titles, goals, outline snapshots or other learner text. Manual controls SHALL NOT duplicate generation outcomes or emit model usage.

#### Scenario: Outline moved
- **WHEN** a learner moves a unit or lesson
- **THEN** one `ui_control_activated` event carries `surface=outline` and `control_id=outline_move` without its title or goal

### Requirement: Subscription lifecycle events
The catalogue SHALL include `subscription_renewed` and `subscription_cancelled`
(plan_id, interval, channel, billing_period_start/end, entitlement_version),
`subscription_payment_failed` (plan_id, channel, reason_code), and
`entitlement_changed` (plan_id, entitlement_version, reason_code). Renewals SHALL
be deduplicated per funded term, failures per canonical unpaid invoice, and
cancellations per canonical state transition. Events SHALL use the transactional
financial outbox and UUID identity, common properties, opt-out and no learner text.

#### Scenario: Stale failure notification
- **WHEN** a failed notification resolves to a canonical paid invoice
- **THEN** no payment-failed event is emitted

### Requirement: Funded subscription financial events
The catalogue SHALL include `plan_purchased` (plan_id, interval, channel,
amount_minor, currency, billing_period_start, billing_period_end,
price_table_version), `subscription_grant_issued` (plan_id, amount, period_key,
price_table_version), and `entitlement_changed` (plan_id, entitlement_version).
These events MUST be created in the funding, scheduled-grant or verified revocation transaction, deduplicated by funded
service-month business key and delivered through the financial outbox. They MUST
inherit UUID identity, common properties, opt-out and content-free allowlists.

Annual keys SHALL include the funded term and service-month index 0–11; a term revocation SHALL create one entitlement change without learner content.

#### Scenario: Duplicate monthly invoice notifications
- **WHEN** paid and payment-succeeded events describe one funded service month
- **THEN** one occurrence of each financial event is created and delivered

### Requirement: Identity and common properties
Events SHALL use the internal Learner id (UUID) as `distinct_id`, never email. Every event MUST include `platform` (`desktop`, `ios`, `android`, `web`), `app_version` and `source` (`client`, `api` or `engine`). Events about a Course or Lesson MUST include `course_id` and `lesson_id` when known.

#### Scenario: Event without email
- **WHEN** any event is sent
- **THEN** `distinct_id` is the Learner UUID and no email or free text is in properties

### Requirement: Privacy rules
Events MUST NOT contain note text, chat content, profile text, Slide HTML or answers. A Learner opt-out MUST stop all events and clear pending client events. Server-side events SHALL be sent by the API through a non-blocking queue so analytics failure never breaks a request. The catalogue includes `analytics_opt_out_changed` (enabled) and `ui_appearance_changed` (mode); the opt-out transition event is delivered only when the learner remains opted in.

#### Scenario: PostHog down
- **WHEN** PostHog is unreachable
- **THEN** the API request still succeeds and events are dropped or retried in the background

### Requirement: Course and lesson events
The catalog MUST include: `course_preview_requested` (topic_length, has_profile), `course_preview_viewed` (preview_id: opaque financial job UUID, duration_ms; ephemeral preview only), `course_outline_generated` (course_id, unit_count, lesson_count, provider, model, latency_ms), `course_enrolled` (course_id), `course_generation_failed` (stage: preview|outline, error_code, preview_id when preview, duration_ms), `lesson_generation_requested` (course_id, lesson_id), `lesson_generated` (lesson_id, slide_count, provider, model, latency_ms), `lesson_started`, `slide_viewed` (index, watch_ms, offline), `slide_replayed`, `slide_paused`, `lesson_completed` (xp, duration_ms), `lesson_player_error` (error_code, slide_id), `lesson_test_started`, `lesson_test_submitted`, `lesson_test_passed` (score, duration_ms), `character_rendered` (speaker_key, character_version, rig_version, expression, offline; deduplicated per Slide speaker visit) and `character_render_fallback` (reason_code, offline). Character events MUST exclude character text, bio, persona, dialogue and SVG content.

#### Scenario: Catalog check
- **WHEN** a feature emits an event
- **THEN** its name and properties appear in the catalog or the change adds them

### Requirement: Gamification and social events
The catalog MUST include: `xp_earned` (amount, course_id, lesson_id, event_id, local_date, config_version, perfect), `daily_goal_met` (goal_xp, event_id, local_date), `lesson_completed` (xp, duration_ms, course_id, lesson_id, event_id, local_date, config_version, perfect), `streak_extended` (streak_length), `streak_broken` (previous_length), `streak_freeze_earned` (reason, streak_length, local_date, event_id), `streak_freeze_used` (remaining, protected_on, streak_length, event_id), `league_promoted` and `league_demoted` (event_id, week_start, league, rank, from_tier, to_tier). Friend request, Friend streak and nudge events are defined in the Friend event, Friend streak event and Friend nudge event catalogues.

#### Scenario: Streak broken
- **WHEN** a Streak resets
- **THEN** `streak_broken` carries `previous_length`

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

### Requirement: Pricing exposure events
The catalogue SHALL include `pricing_version_exposed` with `pricing_version` and `surface` (`paywall`, `quote`, `pack_list`, `plan_list`). The client SHALL emit at most once per authenticated Learner session, surface and version through the existing analytics wrapper, with UUID identity, platform, app_version and source. Opt-out SHALL suppress delivery; no learner text, provider mapping or secrets may be included. Signed-out catalogue browsing SHALL not fabricate a Learner identity.

#### Scenario: Repeated price view
- **WHEN** a signed-in Learner opens the same price surface twice in one session
- **THEN** one exposure event reaches PostHog, unless analytics is disabled

### Requirement: Purchase recovery events
The catalogue SHALL include client-owned `paywall_viewed` (placement, feature, balance, plan_id, price_table_version), `credits_upsell_shown` (feature, next_daily_grant_at, price_table_version), `ui_purchase_option_selected` and `ui_checkout_requested` (offer_id, billing_period, price_table_version). These events SHALL include UUID/common properties and respect opt-out. No draft, raw note, prompt, URL or callback parameter SHALL be included. Illustrative checkout SHALL NOT emit a financial purchase event.

#### Scenario: Pending preview
- **WHEN** a learner requests an illustrative checkout
- **THEN** only the UI request is captured, with configured identifiers and no payment success event

### Requirement: Pricing lifecycle events
The catalogue SHALL include `pricing_version_created`, `pricing_version_approved`, `pricing_version_rejected`, `pricing_version_activated`, `pricing_version_rolled_back` and `price_table_activated` with `pricing_version`, `change_class` (`adverse` or `non_adverse`). Actor identity SHALL appear only as the distinct ID. The API SHALL emit them through the existing UUID/common-properties wrapper. Events MUST NOT include reasons, full pricing content, credentials or provider mappings. Server opt-out SHALL suppress delivery. Activation events SHALL be recorded once per version in Postgres before delivery.

#### Scenario: Pricing lifecycle
- **WHEN** a pricing administrator creates, approves or rejects a version
- **THEN** the corresponding lifecycle event is emitted with common properties and no private reason text

#### Scenario: Scheduled activation
- **WHEN** database time reaches an approved version's effective instant
- **THEN** activation is durably recorded once and both activation events are emitted through the existing wrapper without private pricing data

### Requirement: LLM analytics
Every Engine call SHALL produce a PostHog `$ai_generation` event from the API with `$ai_provider`, `$ai_model`, `$ai_input_tokens`, `$ai_output_tokens`, `$ai_latency`, `feature`, `fallback_used`, `$ai_is_error`, and sanitized `error_code`. Error codes MUST be bounded identifier tokens (such as `rate_limited`, `provider_outage`, `invalid_schema`, `timeout`, `expensive_fallback`, `cancelled`, `stream_error`) and MUST NOT contain raw provider diagnostics, prompt text, or completion content. Prompt and completion text MUST NOT be captured.
The existing setup-chat emitter's `provider`, `model`, `input_tokens`,
`output_tokens`, `latency_ms` and `task` properties SHALL remain compatibility
aliases; the adapter SHALL also supply their canonical fields above. Neither
representation permits prompt, completion or learner text.

#### Scenario: Fallback call
- **WHEN** the Engine used the fallback provider
- **THEN** `$ai_generation` has `fallback_used` true and the real provider and model

#### Scenario: Provider error
- **WHEN** a provider call fails with an outage or timeout
- **THEN** `$ai_generation` records `$ai_is_error` true and the sanitized `error_code` without free text

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

### Requirement: Payment reconciliation operations analytics
The API SHALL emit `payment_reconciliation_failed` with channel `stripe` and a
closed `reason_code`: `provider_unavailable`, `invalid_evidence`, `dead_letter`, or
`mismatch`. These operations events SHALL use the reserved system UUID
`00000000-0000-4000-8000-000000000011`, source `api`, platform `web`, and build
`app_version`. Global opt-out MUST suppress delivery through the existing wrapper.
No provider identifiers, payloads, errors or learner content SHALL be sent.
Independent operational alerts MUST remain available when analytics is disabled.

#### Scenario: Unresolved financial difference
- **WHEN** canonical TEST provider state cannot reconcile to retained financial business keys
- **THEN** an operational alert is raised and the content-free failure event is delivered unless opted out

#### Scenario: Analytics disabled
- **WHEN** reconciliation fails with analytics opted out
- **THEN** the operational alert remains available and no PostHog event is delivered

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

### Requirement: Versioned financial job event catalogue
The API event catalogue SHALL include `credits_reserved`, `credits_settled`,
`credits_released`, `credits_insufficient` and `credits_exhausted` with feature,
amount, balance, estimate, bucket and pricing_version, and `$ai_generation` with provider, model, attempt,
input/output/cache-write/cache-read token counts, newly synthesized character
count, latency, provider_cost_nanos, fallback_used and usage_available. A transactionally unique outbox
key SHALL prevent repeated operation events. Delivery SHALL use the existing
nonblocking wrapper, honor learner/global opt-out, carry Learner UUID/common
properties, and exclude prompt/completion/profile/note/chat text. Operational
cost and concurrency state SHALL remain independent of event delivery.

#### Scenario: Repeated settlement
- **WHEN** a worker repeats finalization after a network interruption
- **THEN** one financial outcome and one set of per-attempt events exist

### Requirement: Personalized preview event delivery
Preview requested/viewed/failed events SHALL be API-owned and allowlisted, include UUID identity, source/platform/app_version, and honor current server opt-out. Requested events SHALL carry topic_length and has_profile only; viewed events SHALL identify an ephemeral preview by its opaque financial job UUID and duration_ms. Failure stage SHALL be preview with a fixed error_code, never provider diagnostics. Every attempted generation SHALL retain actual usage in the existing $ai_generation event. Preview text, profile, brief, slide HTML and outline titles MUST NOT enter analytics.

#### Scenario: Preview discarded
- **WHEN** a learner discards a paid preview
- **THEN** the API-owned viewed event remains derived analytics and no Course or enrollment is created

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

### Requirement: Private Character creation event catalogue
The API SHALL emit `character_create_quoted` (operation, credits, pricing_version),
`character_created` and `character_regenerated` (character_id, character_version,
`rig_version, voice_key, credits, pricing_version), `character_rejected` (operation,
reason_code) and `character_create_failed` (operation, reason_code, fallback_used).
Outcome events MUST be deduplicated by operation/result through the transactional
outbox. `$ai_generation` SHALL use feature=character_create or character_screen
and actual per-attempt provider/model/token/cache/latency/error/fallback metadata,
including late attempts after cancellation. UUID/common properties, consent,
non-blocking delivery and person deletion apply. Prompts, names, bios, backstories,
personas, SVG and dialogue MUST NOT be captured.

#### Scenario: Failed regeneration
- **WHEN** bounded providers fail and the old Character stays current
- **THEN** one failure event and each actual attempt are recorded, with no paid success event or learner text

### Requirement: Authorized player event projection
The catalogue SHALL define `lesson_started` with `lesson_id` and `course_id`, and `lesson_player_error` with `lesson_id`, optional `slide_id`, and fixed `error_code` (`manifest_unavailable`, `document_unavailable`, `invalid_manifest`, `document_timeout`, `offline`). Both SHALL use authenticated Learner UUID/common properties and existing opt-out. The wrapper SHALL discard all other properties, including titles, narration, learner text, HTML and URLs. E3-01 SHALL emit no completion or slide-view events.

#### Scenario: Content-free playback delivery
- **WHEN** the authenticated player starts or fails and analytics is enabled
- **THEN** the PostHog test double receives only registered IDs/codes and common properties; opted-out learners emit nothing

### Requirement: Opened lesson cache recovery catalogue
The existing lesson_player_error allowlist SHALL include fixed codes offline_uncached, cache_unavailable and checksum_mismatch. The existing ui_recovery_selected allowlist SHALL register surface=lesson_player, error_code=offline_uncached and action=retry|exit. Both events SHALL preserve UUID/common properties and opt-out, and SHALL discard HTML, URLs, titles, narration and learner text.

#### Scenario: Uncached offline recovery
- **WHEN** an opted-in learner selects Retry or Exit from the offline lesson recovery
- **THEN** the registered recovery event is delivered with only fixed identifiers and common properties

### Requirement: Lesson test retry and completion telemetry
The catalogue SHALL include API-owned `retry_variant_pool_exhausted` with course_id, lesson_id, topic_id and fixed error_code=pool_exhausted. Exhaustion SHALL NOT be reported as a provider generation failure. `retry_variant_served` SHALL use variant_source for pool provenance and retain the common source=api property. Lesson test lifecycle events SHALL be emitted only after their transaction commits, with no duplicate client lifecycle events. `lesson_test_passed` SHALL emit once when the completion outbox entry is first inserted, with measured session duration_ms, first-try score and validated app_version (or unknown when absent). All properties MUST pass the allowlist, honor opt-out and exclude learner text and selected answer values.

#### Scenario: Exhaustion is free pool reuse
- **WHEN** a pre-generated retry pool is exhausted and reused for free
- **THEN** retry_variant_pool_exhausted and retry_variant_served identify the topic and source without reporting a provider failure or exposing question text

#### Scenario: Replayed completion
- **WHEN** a final answer or completed session is replayed
- **THEN** no duplicate lesson_test_passed event is emitted and the original summary is retained

### Requirement: Lesson tutor sheet events
`tutor_message_sent` SHALL carry boolean `has_lesson_context`. `ui_sheet_opened` SHALL accept `sheet_id=lesson_tutor`. `ui_recovery_selected` SHALL accept `surface=lesson_tutor` with `action` one of `retry`, `reload`, `top_up`, `read_transcript`. These events MUST carry the Learner UUID and common properties (`platform`, `app_version`, `source`), honour analytics opt-out and never include message, draft or lesson text.

#### Scenario: Ask about the paused lesson
- **WHEN** the Learner opens the lesson tutor sheet and sends a question
- **THEN** `ui_sheet_opened` (`sheet_id=lesson_tutor`) and `tutor_message_sent` (`has_lesson_context=true`) are delivered with common properties and no question text

#### Scenario: Opted out
- **WHEN** the Learner has opted out
- **THEN** none of these events is delivered

### Requirement: Private Course steering event catalogue
The API SHALL deliver steering events through its existing consent-aware PostHog wrapper, with a UUID Learner distinct id and `source`, `platform`, `app_version` common properties. `course_instruction_added` SHALL contain `course_id`, closed `scope`, `length_bucket` (`1_100`, `101_250`, `251_500`) and `is_course_copy`; `course_instruction_rejected` SHALL contain `course_id` and a closed `reason_code`; `course_instruction_applied` SHALL contain `course_id` and `lesson_id`. `course_instruction_edited` SHALL contain `course_id`, closed `scope`, `length_bucket` and closed `changed` (`text`, `scope`, `both`); `course_instruction_deleted` SHALL contain `course_id` and closed `scope`. Both are emitted only after the change commits and never carry instruction text. Platform screening SHALL report `$ai_generation` with `feature=steer_screen`, provider/model, token usage, latency, fallback and success flags. No event SHALL contain instruction text, prompts or Course titles. Opt-out SHALL suppress delivery.
The existing `ui_sheet_opened` event SHALL accept `sheet_id=course_steering` for the instruction panel, with the same common properties and privacy rules.

Re-plan emits `course_replan_quoted` with `course_id`, `credits`, `pricing_version`; `course_replan_proposed` with `course_id`, `proposal_id`, `lessons_added`, `lessons_removed`, `lessons_changed`; `course_replan_confirmed` with `course_id`, `proposal_id`, `outline_version`, `lessons_added`, `lessons_removed`, `lessons_changed`; `course_replan_discarded` with `course_id`, `proposal_id`; and `course_replan_failed` with `course_id` and closed `reason` (`quote_invalid`, `insufficient_credits`, `protected_lesson_changed`, `invalid_proposal`, `replan_unavailable`, `generation_failed`). `$ai_generation` accepts `feature=steer_replan`, provider/model, token counts, latency, fallback and success flags and the Pricing version. UUID and common properties, opt-out and text exclusion apply to all these events.

#### Scenario: Edit and delete
- **WHEN** a Learner edits or deletes an instruction
- **THEN** PostHog receives `course_instruction_edited` (changed, scope, length bucket) or `course_instruction_deleted` (scope), never the text

#### Scenario: Safe instruction saved
- **WHEN** a Learner saves an allowed next-lesson instruction
- **THEN** PostHog receives `course_instruction_added` with its length bucket, never its text

#### Scenario: Analytics opt-out
- **WHEN** a Learner has opted out of analytics
- **THEN** steering and screening events are not delivered to PostHog

#### Scenario: Proposal confirmed
- **WHEN** a Learner confirms an unexpired outline proposal
- **THEN** PostHog receives `course_replan_confirmed` with proposal ID, new outline version and change counts, without outline or instruction text

#### Scenario: Proposal discarded
- **WHEN** a Learner discards an outline proposal
- **THEN** PostHog receives `course_replan_discarded` with proposal ID, without outline or instruction text

#### Scenario: Paid proposal delivered
- **WHEN** a valid remaining-outline proposal is stored and settled
- **THEN** PostHog receives `course_replan_proposed` and `$ai_generation` without outline or instruction text

#### Scenario: Fork confirmed
- **WHEN** a Learner confirms the fork warning and saves the first instruction on a Published course copy
- **THEN** PostHog receives `course_forked_from_published` (`course_id`, numeric `fork_lesson_index`; 0 when every Lesson was already unlocked) once, and `course_instruction_added` with `is_course_copy=true`, never the text; declining sends neither

### Requirement: Screened review outcomes
The API SHALL catalogue and emit review_submitted (course_id, review_id, rating, version, verdict), review_deleted (course_id, review_id) and $ai_generation with feature screen_review. Identifiers SHALL be UUIDs; ratings SHALL be stars and verdicts closed enums. Events SHALL honor opt-out and SHALL NOT include review text or attribution.

#### Scenario: Review saved
- **WHEN** a learner submits or edits a review
- **THEN** the API sends only identifiers, stars, version and screening verdict through the consent gate

### Requirement: Report outcomes
The API SHALL catalogue and emit course_reported (report_id, course_id when known, reason, target_type) and course_hidden_by_reports (course_id, reporter_count). Identifiers SHALL be UUIDs, reasons and target types closed enums and the count a bounded integer. Events SHALL honor opt-out and SHALL NOT include report text, usernames or any reporter attribution to the owner.

#### Scenario: Third reporter
- **WHEN** a third distinct learner reports a course
- **THEN** the API sends course_reported and one course_hidden_by_reports through the consent gate, with no report text

### Requirement: Moderation outcomes
The API SHALL catalogue and emit moderation_action (course_id when known, action, reason_code, target_type), moderation_appeal (course_id when known, outcome filed, upheld or overturned) and course_visibility_changed with from, to and cause moderation. Identifiers SHALL be UUIDs and the rest closed enums. Events SHALL honor opt-out and SHALL NOT include reason text, appeal text, usernames or moderator identity.

#### Scenario: Takedown
- **WHEN** a moderator takes a course down
- **THEN** the API sends moderation_action and course_visibility_changed through the consent gate with no reason text

### Requirement: Narration events
The narration events are defined in the Narration analytics events and Narration control and caption telemetry requirements. Narration text MUST NOT be sent to PostHog.

#### Scenario: Cache hit
- **WHEN** narration audio for an identical hash already exists
- **THEN** `narration_cache_hit` is emitted and no `narration_generated` event follows

Lesson test lifecycle events are emitted by the committing API transaction only. `duration_ms` comes from the session creation time, `score` is the fraction of original questions answered correctly on their first submission, and `app_version` is the validated client build version or `unknown`. Retry delivery uses `variant_source` to distinguish pool reuse from the common event `source=api`. Learner prompts, choices, explanations and selected answer values MUST NOT be sent.

### Requirement: Steering output review signal
The API SHALL emit `course_instruction_output_flagged` through the same consent-aware wrapper when generated Lesson output is refused for echoing the prompt or an instruction, for a weakened Lesson test or for malformed output. It SHALL contain `course_id` and a closed `reason_code` (`prompt_leak`, `test_weakened`, `malformed_output`) and never instruction text, generated text or prompts. `course_instruction_rejected` SHALL accept the same closed reasons plus the screening reasons already listed.

#### Scenario: Leaked prompt in generated output
- **WHEN** a generated Lesson contains the system policy, the envelope, the data delimiters or a long instruction verbatim
- **THEN** the Lesson is not delivered, PostHog receives `course_instruction_output_flagged` with `reason_code=prompt_leak` and no text, and the reservation is released

### Requirement: Character event catalog
The PostHog catalog SHALL include the events and properties below. All events MUST include existing identity/common properties (`distinct_id` as Learner UUID, `platform`, `app_version`, `source`), and `course_id`, `lesson_id`, `slide_id` when known. `operation` MUST be `create`, `regenerate` or `manual_edit`; `cast_source` MUST be `default`, `custom`, `mixed` or `published`. Identifiers/versions MUST be opaque IDs or integers; `fields_changed` MUST contain schema field names only; reason codes MUST be bounded enums rather than provider errors or user content.

| Event | Source | Properties |
|---|---|---|
| `character_create_quoted` | api | operation, credits, pricing_version |
| `character_created` | api | character_id, character_version, rig_version, voice_key, credits, pricing_version |
| `character_regenerated` | api | character_id, character_version, rig_version, voice_key, credits, pricing_version |
| `character_updated` | api | character_id, character_version, fields_changed |
| `character_deleted` | api | character_id |
| `character_rejected` | api | operation, reason_code |
| `character_create_failed` | api | operation, reason_code, fallback_used |
| `course_cast_selected` | api | cast_source, supporting_count, cast_version |
| `lesson_cast_applied` | api | cast_version, cast_source, supporting_count, fallback_used |
| `character_rendered` | client | speaker_key, character_version, rig_version, expression, offline |
| `character_render_fallback` | client | reason_code, offline |
| `character_reaction_shown` | client | speaker_key, expression, reaction_trigger (`answer_correct`, `answer_incorrect`, `test_passed`), fallback_used; once per Lesson/question result |
| `tutor_character_selected` | client | speaker_key, character_version, cast_source |
| `tutor_character_fallback` | api | reason_code, phase (`before_stream`) |
| `character_voice_fallback` | api | speaker_key, reason_code, voice_config_version |
| `character_snapshot_published` | api | course_version, character_count, custom_count |

#### Scenario: Successful Character creation funnel
- **WHEN** a Learner accepts a Character quote and the job succeeds
- **THEN** one `character_created` event follows the quote event with the actual settled Credits and pricing version

#### Scenario: Manual editing event
- **WHEN** a free field edit succeeds
- **THEN** `character_updated` lists only field names and no creation/spend event is emitted for the edit

E17-03 emits only `character_updated`, `character_deleted`,
`character_rejected(operation=manual_edit)`, `course_cast_selected` and per-attempt
`$ai_generation(feature=character_screen)` from the API. `fields_changed` is an
allowlist of `info.name`, `info.bio`, `info.backstory`, `personality.traits`,
`personality.speaking_style`, `personality.persona_prompt`, `appearance.body_shape`,
`appearance.palette`, `appearance.eyes`, `appearance.mouth`,
`appearance.accessories`, `voice_key`. It never contains field values or dynamic
schema keys. Compare-and-swap version replacement and a successful deletion emit
their outcome once; denied repeats emit no success. Every actual model attempt
records provider/model/token/cache/latency/error/fallback metadata. No model call
is inferred from a deterministic rejection. Existing queued delivery and account
person-deletion apply; learner profile consent suppresses these events.

### Requirement: Character analytics privacy and delivery
Character events and `$ai_generation` MUST exclude raw prompts, names, bios, backstories, persona text, speaking-style text, dialogue, answers, spec JSON/SVG and credentials. Existing opt-out, non-blocking delivery and PostHog person deletion MUST apply. API job events MUST be deduplicated by operation/result ID; render events MUST be once per Slide speaker per visit and publication events once per Course version. Every model attempt MUST report actual usage, provider, model, latency, errors and fallback status with a bounded feature key, including platform-paid screening; prompt/completion capture MUST remain disabled.

#### Scenario: Analytics unavailable
- **WHEN** PostHog is down during creation or grading
- **THEN** the operation continues normally and a background queue handles event delivery without leaking content

#### Scenario: Persona contains personal info
- **WHEN** a Character spec includes learner-provided bio text
- **THEN** neither the bio nor any derived free-text summary appears in product or LLM analytics

### Requirement: Character analytics data backup and retention
PostHog MUST remain a derived store, never the sole source of Character creation, Cast selection or snapshot state. That state MUST live in Postgres with nightly dumps and required versioned S3 assets in nightly protected asset backups per `custom-characters`. Event retention MUST follow the existing analytics policy; account deletion MUST request PostHog person deletion. A raw-content analytics archive MUST NOT be created; product records MUST follow the Character and E15 deletion policies.

#### Scenario: PostHog data lost
- **WHEN** analytics data is unavailable after restore
- **THEN** Character ownership, Casts and published snapshots still resolve entirely from restored product records and assets

### Requirement: Character analytics reliability and tests
Emitters MUST have unit tests and e2e funnel assertions with a PostHog test double, including opt-out, no-content capture and idempotent job delivery. Analytics collection MUST NOT introduce model calls, model timeouts or Credits spend; AI-derived results MUST already pass `custom-characters` Golden set thresholds and bounded fallback before success events are emitted. Invalid or failed mapping MUST emit rejection/failure rather than a paid creation success.

#### Scenario: Failed mapping not counted as success
- **WHEN** all bounded mapping attempts fail and the Default cast is offered
- **THEN** the analytics test observes `character_create_failed` and no `character_created` event or successful spend

### Requirement: Profile completion event
The API SHALL emit `learner_profile_updated` with changed field names only and `onboarding_completed` without domain properties after committed profile confirmation. Both events SHALL use the learner UUID, API source and common platform/app version properties, and SHALL respect analytics opt-out.

#### Scenario: First explicit completion
- **WHEN** a learner first saves or skips setup
- **THEN** `onboarding_completed` is delivered once, and subsequent saves do not emit it again

#### Scenario: Profile field update
- **WHEN** a learner changes one or more profile values
- **THEN** `learner_profile_updated` includes only the names of effectively changed fields

### Requirement: Consent-aware client analytics boundary
The client SHALL identify learners only by internal UUID and include platform, app_version and source on allowlisted events. Opting out SHALL immediately stop captures and clear pending learner events. Analytics failures SHALL NOT block settings updates or other requests.

#### Scenario: Opt out immediately
- **WHEN** a learner turns analytics off
- **THEN** no subsequent client or API event is delivered for that learner and pending client events are cleared

#### Scenario: Settings events
- **WHEN** an opted-in learner changes appearance
- **THEN** `ui_appearance_changed` contains only the stable appearance enum and common properties

### Requirement: Consent event catalog
The analytics catalogue SHALL include `analytics_opt_out_changed` and `ui_appearance_changed`. The opt-out transition event SHALL be delivered only when the learner remains opted in; opting out suppresses its delivery. No preference values that expose profile or learner text SHALL be sent.

#### Scenario: Opt-out transition
- **WHEN** a learner turns analytics off
- **THEN** capture is stopped and no opt-out event is transmitted after the change

### Requirement: Analytics backup and retention
PostHog SHALL be a derived store and SHALL NOT be the only copy of a preference or consent value. The source values SHALL remain in the backed-up learner profile and be deleted with that row.

#### Scenario: Delete analytics preferences
- **WHEN** a Learner profile is deleted
- **THEN** its persisted consent and settings are removed with the profile

### Requirement: Independent operational signals
Operational request, RPC and job counts, durations, errors and alert counts SHALL
be exposed as fixed-label metrics independent of PostHog and analytics consent.
No new learner analytics event SHALL be introduced. The existing backup_failed
catalogue event SHALL retain its UUID, common properties and safe numeric outcome
properties. Analytics adapters MUST enqueue without blocking and MUST discard
uncatalogued or private properties. Learner consent checks SHALL remain enforced.

#### Scenario: Analytics outage and opt-out
- **WHEN** PostHog fails or global analytics opt-out is enabled
- **THEN** requests and jobs continue and operational alerts still increment

#### Scenario: Private event delivery
- **WHEN** backup_failed reaches the PostHog test double
- **THEN** it contains the reserved UUID, common properties and outcome fields only

### Requirement: Compatible AI telemetry metadata
The analytics adapter SHALL retain the existing setup-chat provider, model,
input_tokens, output_tokens, latency_ms and task compatibility properties and
SHALL also normalize them to the canonical $ai_generation catalogue fields.
No prompt or completion SHALL be captured. This changes no model behavior.

#### Scenario: Existing setup chat analytics
- **WHEN** the existing setup-chat emitter reports numeric usage metadata
- **THEN** canonical properties and compatibility aliases contain only usage and fixed identifiers

### Requirement: Course brief privacy-safe event catalogue
The client SHALL emit existing `ui_control_activated` for `quick_reply`, `skip`, `form_fallback` through the guided-chat surface and `confirm_brief` through the course_brief surface. Mobile opening SHALL emit `ui_sheet_opened(sheet_id=course_brief)`. The API SHALL emit `$ai_generation(task=course_brief_chat)` per attempt with pricing_version, provider, model, token counts, latency_ms, fallback_used, succeeded and usage_available. Events MUST use authenticated Learner UUID and common platform/app_version/source properties, honor opt-out and remain non-blocking. Unknown usage MUST be labelled unavailable. Learner text, brief values and transcripts MUST NOT enter analytics. Paid preview events remain outside clarification.

#### Scenario: Learner confirms a brief
- **WHEN** a signed-in opted-in Learner selects Confirm brief
- **THEN** ui_control_activated contains control_id=confirm_brief and surface=course_brief with common properties
- **AND** no brief text is captured

#### Scenario: Opted-out learner clarifies
- **WHEN** the Learner has opted out
- **THEN** neither guided controls nor model attempts produce a PostHog event

### Requirement: Course switcher analytics catalogue
The catalog SHALL include `ui_navigation_selected` (destination) and `learner_profile_updated` (fields_changed). Course switching and level updates SHALL emit these events with platform, app_version and source common properties, respecting analytics opt-out and excluding learner private text.

#### Scenario: Course switched
- **WHEN** a learner selects another course
- **THEN** ui_navigation_selected is emitted with destination

#### Scenario: Level override updated
- **WHEN** a learner overrides or resets a course level
- **THEN** learner_profile_updated is emitted with fields_changed containing level

### Requirement: Course recovery analytics
The analytics catalogue SHALL include `ui_recovery_selected` with properties `surface` (`course_create`, `courses`), `error_code`, `action`, `course_id`, and `screen_id`. When course outline creation fails or is retried, or course removal is handled, the corresponding recovery action SHALL be emitted through PostHog wrappers respecting learner opt-out, UUID identity, and excluding learner text. The API allowlist SHALL retain `ui_recovery_selected` and `course_generation_failed`.

#### Scenario: Creation failure retry selected
- **WHEN** a learner selects retry after course creation failure
- **THEN** `ui_recovery_selected` is emitted with `surface=course_create`, `action=retry`, and no private prompt or outline text

### Requirement: Bookmark event catalogue
The analytics catalogue SHALL include `bookmark_added`, `bookmark_removed`, and `bookmark_opened` with `target_type` (`slide` or `lesson`), `course_id`, `lesson_id`, and `slide_id`. All events SHALL use Learner UUID distinct IDs and common properties (`platform`, `app_version`, `source`), and respect analytics opt-out. Private learner text and slide HTML MUST NOT enter PostHog analytics under any circumstance.

#### Scenario: Bookmark added event
- **WHEN** a Bookmark is created
- **THEN** `bookmark_added` is emitted with `target_type`, `course_id`, `lesson_id`, and `slide_id`

#### Scenario: Bookmark removed event
- **WHEN** a Bookmark is removed
- **THEN** `bookmark_removed` is emitted with `target_type`, `course_id`, `lesson_id`, and `slide_id`

#### Scenario: Bookmark opened event
- **WHEN** a Bookmark is opened
- **THEN** `bookmark_opened` is emitted with `target_type`, `course_id`, `lesson_id`, and `slide_id`

### Requirement: Quick note enrichment events
The analytics catalogue SHALL include `quick_note_enriched` and `quick_note_enrichment_failed` events emitted upon note enrichment completion or terminal failure. Both events SHALL include standard learner distinct ID and common properties (`platform`, `app_version`, `source`), and respect analytics opt-out. Note text, summary text, and tags MUST NOT enter PostHog analytics under any circumstance.

#### Scenario: Note enrichment completed
- **WHEN** a note is successfully enriched with summary and tags
- **THEN** `quick_note_enriched` is emitted without note or summary text

#### Scenario: Note enrichment failed
- **WHEN** a note enrichment job encounters a terminal failure
- **THEN** `quick_note_enrichment_failed` is emitted without note or summary text

### Requirement: Tutor turn events
The catalogue SHALL include API-owned `tutor_message_sent` (course_id, class, message_length), `tutor_reply_completed` (course_id, class, model, latency_ms, input_tokens, output_tokens) and `tutor_reply_failed` (course_id, class, error_code, interrupted, latency_ms), plus UUID/common properties. Events MUST honor current opt-out and MUST NOT carry message or reply text. Each settled tutor reply SHALL also produce the existing `$ai_generation` event from credit settlement.

#### Scenario: Private tutor turn
- **WHEN** a reply completes or is interrupted
- **THEN** the allowlisted event carries identifiers and counts only

### Requirement: Tutor stop and failure events
The catalogue SHALL include API-owned `tutor_reply_stopped` (course_id, class, reason `stop|disconnect`, delivered_chars, latency_ms) and `tutor_reply_failed` with `error_code`, `interrupted`, `retryable` and `latency_ms`, plus UUID/common properties. `$ai_generation` from tutor settlement SHALL carry the actual provider, model, `fallback_used`, token usage, `usage_available` and `usage_estimated`. Events MUST honor opt-out and MUST NOT carry message or reply text.

#### Scenario: Stopped reply
- **WHEN** a Learner stops a reply
- **THEN** `tutor_reply_stopped` carries identifiers and counts only and the allowlist drops any text property

### Requirement: Tutor context and cache properties
`tutor_reply_completed` SHALL carry `envelope` (`short|long`), `cache_class` (`cold|warm|none`), `summary_count`, `excerpt_count`, `context_tokens`, `cache_read_tokens` and `cache_write_tokens`; tutor `$ai_generation` SHALL carry `envelope` and `cache_class`. Properties are ids and counts; prompts, excerpts and replies MUST NOT be sent, and opt-out is honoured.

#### Scenario: Cold-cache reply
- **WHEN** the provider reports cache writes and no reads
- **THEN** the events carry `cache_class=cold` and counts only

### Requirement: Username event catalogue
The analytics catalogue SHALL include `username_set` (integer `username_length`) and `username_rejected` (`reason_code` restricted to `username_invalid|username_taken|username_reserved|username_not_allowed|username_already_set`). Both SHALL use Learner UUID distinct IDs and common properties and respect analytics opt-out. Usernames, display names and other Learners' identifiers MUST NOT be sent.

#### Scenario: Rejected claim event
- **WHEN** a claim for a reserved name is refused
- **THEN** `username_rejected` is emitted with `reason_code=username_reserved` and no name text

### Requirement: Rename and search event catalogue
The analytics catalogue SHALL include `username_changed` (integer `username_length`), `username_searched` (integer `query_length`, integer `result_count`) and `privacy_setting_changed` (`setting` in `discoverable|request_policy|show_streak_to_friends`, `value` in `on|off|everyone|friends_of_friends|nobody`). They SHALL use Learner UUID distinct IDs and common properties and respect analytics opt-out. Usernames, display names and search text MUST NOT be sent.

#### Scenario: Search event
- **WHEN** a Learner searches by prefix
- **THEN** `username_searched` has the query length and result count and no text

### Requirement: Save and delete thread events
`quick_note_created` SHALL carry `entry_point=chat` and `length` for a note saved from a reply, once per note (not on a replayed save). The client SHALL emit `ui_control_activated` with `surface=tutor_chat` and `control_id=delete_thread` when the Learner confirms deletion. Events MUST carry the common properties and Learner UUID, honour opt-out and never include message, note or thread text.

#### Scenario: Opted out
- **WHEN** the Learner has opted out
- **THEN** neither event is delivered

### Requirement: Friend event catalogue
The analytics catalogue SHALL include `friend_request_sent` (`method` in `username|invite_link`), `friend_request_accepted`, `friend_request_declined` and `friend_removed` (no properties besides common ones). They SHALL use Learner UUID distinct IDs and common properties and respect analytics opt-out. Usernames, display names, tokens and request IDs MUST NOT be sent.

#### Scenario: Send by link
- **WHEN** a Learner sends a request through a friend link
- **THEN** `friend_request_sent` has `method=invite_link` and no other learner's data

### Requirement: Block event catalogue
The analytics catalogue SHALL include `learner_blocked` (`was_friend` boolean) and SHALL extend `privacy_setting_changed` with the setting `course_invites_from` (values `friends|everyone|nobody`). Events MUST use Learner UUID distinct IDs and common properties and respect analytics opt-out. Usernames, display names and the blocked Learner's identifier MUST NOT be sent. No unblock event exists.

#### Scenario: Block a friend
- **WHEN** a Learner blocks a friend
- **THEN** `learner_blocked` has `was_friend=true` and no other Learner's data

### Requirement: Screening event catalogue
The analytics catalogue SHALL include `course_screening_completed` (`verdict` pass|needs_review|block|pending_screening, `lessons_sampled`, `finding_count`, `fallback_used`) and `$ai_generation` with `feature=course_screen`. Events MUST use the Learner UUID, common properties and respect opt-out. Course titles, Slide text, findings text and verdict reasons beyond the closed codes MUST NOT be sent. `course_published` MUST NOT be emitted until visibility is live.

#### Scenario: Screened draft
- **WHEN** a draft finishes screening
- **THEN** one `course_screening_completed` and one `$ai_generation` per model attempt are delivered with no Course text

### Requirement: Friend streak event catalogue
The analytics catalogue SHALL include `friend_streak_started` (`event_id`, `local_date`), `friend_streak_extended` (`event_id`, `local_date`, `streak_length`) and `friend_streak_broken` (`event_id`, `previous_length`, `reason` in `missed|friendship_ended`), and `streak_freeze_earned` with `reason=friend_streak`. Events use the Learner's UUID as distinct ID and common properties and respect analytics opt-out. Usernames, display names and the other Learner's identity MUST NOT be sent.

#### Scenario: Extended
- **WHEN** both friends complete on a shared day
- **THEN** each friend's analytics receives `friend_streak_extended` with counts and dates only

### Requirement: Narration analytics events
The catalogue SHALL define `narration_generated` (lesson_id, slide_id, characters, voice_id, model, latency_ms), `narration_cache_hit` (lesson_id, slide_id, model) and `narration_failed` (lesson_id, slide_id, fixed error_code) from the API, and `narration_played` (lesson_id, slide_id, duration_ms) from the client. Synthesis usage SHALL be reported as `$ai_generation` with `feature` `narration`, `narration_characters`, provider, model and latency. Events MUST carry the common properties and a UUID distinct id, MUST be dropped when the Learner opted out, and MUST NOT contain narration text, captions, audio or URLs. Catalogue entries precede emitters in the same change.

#### Scenario: Generated without private text
- **WHEN** synthesis succeeds
- **THEN** PostHog receives `narration_generated` and `$ai_generation(feature=narration)` with counts and ids and no narration text

#### Scenario: Opt-out
- **WHEN** the Learner opted out
- **THEN** no narration event is delivered

### Requirement: Friend nudge event
The analytics catalogue SHALL include `friend_nudge_sent` (`event_id` the nudge UUID, `local_date`) with the sender's UUID as distinct ID and common properties, respecting opt-out. It MUST be emitted once per created nudge, not on a replay or a rejection, and MUST NOT carry usernames, display names or the recipient's identity.

#### Scenario: Nudge sent
- **WHEN** a nudge is created
- **THEN** `friend_nudge_sent` carries UUIDs, date and common properties only

### Requirement: Sharing analytics events
The system SHALL emit:
- `course_visibility_changed` (from: string, to: string, course_id: UUID) — emitted by API on visibility change
- `course_published` (course_id: UUID, version: int, visibility: enum, lesson_count: int) — emitted by API on first publish
- `share_link_created` (course_id: UUID) — emitted by API when a link is created; NO token
- `share_link_revoked` (course_id: UUID) — emitted by API when a link is revoked; NO token
- `share_link_opened` (course_id: UUID, has_account: bool, surface: "web") — emitted by API on public preview; NO token, NO IP

Events MUST NOT contain Share link tokens, plaintext token, Usernames of other Learners, Slide HTML or answers.

#### Scenario: Course published
- **WHEN** an owner publishes a Course with a passing verdict
- **THEN** `course_visibility_changed` and `course_published` and `share_link_created` are each emitted exactly once with no token or Course text

#### Scenario: Link revoked
- **WHEN** an owner revokes the active share link
- **THEN** `share_link_revoked` is emitted with only the course_id; no token or plaintext appears

#### Scenario: Preview opened
- **WHEN** any visitor opens the public preview URL
- **THEN** `share_link_opened` is emitted with course_id, has_account and surface="web"; no token or IP is recorded

### Requirement: Enrollment analytics events
The API SHALL emit `course_enrolled_published` (course_id: UUID of the copy, source_course_id: UUID, entry_point: `link`, version: int) once when a copy is created, and `published_lesson_reused` (course_id: UUID, lesson_id: UUID, version: int) when a reused Lesson becomes available. Events MUST NOT contain tokens, usernames, titles, Slide content or answers, and MUST respect analytics opt-out.

#### Scenario: Enroll event once
- **WHEN** a Learner enrolls twice through a link
- **THEN** `course_enrolled_published` is emitted once with entry_point `link`

### Requirement: On-demand generation analytics event
The API SHALL emit `published_lesson_generated_on_demand` (course_id: UUID of the copy, lesson_id: UUID, version: int) once after a committed settlement of a Lesson generated in a Course copy. `lesson_generated` and `$ai_generation` are emitted by the reused pipeline. Events MUST contain UUIDs, enums and amounts only, never titles, Slide content or answers, and MUST respect analytics opt-out.

#### Scenario: One event per generated Lesson
- **WHEN** a missing Lesson is generated and settled
- **THEN** `published_lesson_generated_on_demand` is delivered once with UUID properties and nothing when the Learner opted out

### Requirement: Marketplace analytics events
The API SHALL emit `marketplace_browsed` (category, language, sort, result_count, has_cursor), `marketplace_searched` (query_length, category, language, sort, result_count) and `course_viewed` (course_id, version, surface `marketplace`). Properties MUST be enums, UUIDs and integers. The search text, titles and descriptions MUST never be sent. The events MUST respect analytics opt-out, and the catalogue MUST list them before an emitter exists.

#### Scenario: Search sends length only
- **WHEN** a Learner searches "rust ownership"
- **THEN** `marketplace_searched` carries `query_length: 14` and no query text, and nothing is sent for an opted-out Learner

### Requirement: Narration control and caption telemetry
The catalogue SHALL define client events `narration_muted` (carrying `lesson_id`, `slide_id`, and boolean `muted`), `narration_speed_changed` (carrying `lesson_id`, `slide_id`, and numeric `speed`), and `captions_toggled` (carrying `lesson_id`, `slide_id`, and boolean `captions`). All events MUST include Learner distinct id, platform, source, app_version, and common properties, MUST honor user opt-out, and MUST NOT contain narration text, captions, audio URLs, or slide content.

#### Scenario: Mute narration
- **WHEN** the Learner toggles the mute control
- **THEN** PostHog receives `narration_muted` with `muted: true` or `false` and no narration text

#### Scenario: Change playback speed
- **WHEN** the Learner selects a playback speed (0.75, 1, 1.25, 1.5)
- **THEN** PostHog receives `narration_speed_changed` with the new speed value

#### Scenario: Toggle captions
- **WHEN** the Learner toggles captions visibility
- **THEN** PostHog receives `captions_toggled` with the current caption visibility state

### Requirement: Refund analytics catalogue
The analytics catalogue SHALL define `credits_refunded` with properties `amount`, `payment_reference`, `reason_code`, `source`, `platform`, and `app_version`. The event MUST be emitted through the transactional financial outbox and deduplicated by reversal operation key. It MUST honor analytics opt-out and MUST NOT contain card numbers, bank details or learner text.

#### Scenario: Verified refund outbox event
- **WHEN** a provider refund notification commits to the database
- **THEN** exactly one `credits_refunded` event is enqueued in the financial outbox with credit amount, payment reference and reason code

### Requirement: Entitlement change on reversal
`entitlement_changed` SHALL be emitted exactly once when a refund or chargeback revokes a subscription term (reason_code `refunded` or `disputed`) and once per operator goodwill waiver with only a `reason_code`; operator identity and free-text notes MUST NOT enter analytics.

#### Scenario: Goodwill waiver analytics
- **WHEN** an operator waives source debt with private notes
- **THEN** the outbox event carries the reason code only and the notes remain in the database audit table

### Requirement: Course invite analytics events
The API SHALL emit `course_invite_sent` (method `username` or `friend`, course_id, invite_id), `course_invite_accepted` and `course_invite_declined` (course_id, invite_id), and `course_enrolled_published` with entry_point `invite`. Properties MUST be UUIDs and enums, never Usernames or titles, and opt-out MUST be honored. The catalogue MUST list the events before an emitter exists.

#### Scenario: Accept emits enrollment
- **WHEN** a Learner accepts an invite
- **THEN** `course_invite_accepted` and `course_enrolled_published` with entry_point `invite` are captured and nothing is sent for an opted-out Learner

### Requirement: Republish and update analytics events
The API SHALL emit `course_republished` (course_id, version, lesson_count, verdict) and `course_update_applied` (course_id, source_course_id, from_version, to_version, kept, added, archived). Properties MUST be UUIDs, enums and counts, never titles, changelog text or Usernames; opt-out MUST be honored and the catalogue MUST list both events before an emitter exists.

#### Scenario: Update emits
- **WHEN** a Learner updates a copy
- **THEN** `course_update_applied` is captured with counts only

### Requirement: Analytics SDK loads after first paint and consent
The client SHALL import and initialise the analytics SDK only after first paint and only after consent is resolved to opted in. No analytics network request SHALL be made before that. Events captured while the SDK is loading SHALL be queued and delivered in capture order once it is ready. Opting out SHALL drop the queue, cancel a pending load and never initialise an SDK that arrives afterwards. Autocapture and automatic pageviews SHALL remain off, and a load failure SHALL NOT block the app.

#### Scenario: Events before the SDK is ready
- **WHEN** an opted-in learner triggers an allowlisted event before the SDK has loaded
- **THEN** the event is delivered after initialisation, in order, with its original properties

#### Scenario: Opted out
- **WHEN** a learner is opted out
- **THEN** the SDK is not loaded, no analytics request is made and queued events are discarded

#### Scenario: Opt-out while loading
- **WHEN** a learner opts out after the SDK started loading but before it initialised
- **THEN** the SDK is not initialised and nothing is sent

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

### Requirement: Learner sign-up analytics
The API SHALL emit `learner_signed_up` once after the transaction creating a Learner commits, with `method` (`email` or `clerk`) and the common properties. Existing Learner sign-ins and failed transactions SHALL NOT emit it. The event SHALL respect current analytics consent and SHALL NOT contain email, name, credentials or other learner text.

#### Scenario: Repeated sign-in
- **WHEN** an existing Learner signs in again
- **THEN** no additional `learner_signed_up` event is emitted

### Requirement: Action quote acceptance analytics
The API SHALL emit `action_quote_accepted` once when acceptance of an Action quote first commits, with `quote_id`, `feature` and `pricing_version` and common properties. Replays and rejected or expired quotes SHALL NOT emit acceptance. Delivery SHALL honor consent and exclude quote inputs, model mappings and learner text.

#### Scenario: Replayed acceptance
- **WHEN** an accepted Action quote is accepted again
- **THEN** no second acceptance event is emitted

### Requirement: Action quote exposure analytics
The client SHALL emit `action_quote_shown` when it displays an Action quote, with `quote_id`, `feature` and `pricing_version`, once per authenticated Learner session and quote. It SHALL honor consent and exclude action input, learner text and model mappings. Quote exposure and acceptance SHALL be joinable by quote ID.

#### Scenario: Repeated display
- **WHEN** a Learner sees the same Action quote again in the session
- **THEN** no second exposure event is emitted

### Requirement: Example course events
The catalogue SHALL include `example_courses_viewed` (surface: courses|learn_empty|onboarding) and `example_course_enrolled` (slug from the closed list of authored example slugs, surface: courses|learn_empty|onboarding, and the copy's course_id). Events SHALL use UUID/common properties, honor opt-out and consent, and carry no free text, titles or Lesson content. Lesson events keep their existing properties; no content-source field is added because the enrollment event carries the copy's course_id for joining.

#### Scenario: Learner starts an example
- **WHEN** a Learner opens the examples section and enrolls
- **THEN** one viewed event and one enrolled event with closed-enum properties are emitted, and none for an opted-out Learner

### Requirement: Immersive and region analytics catalogue
The catalogue SHALL include:
- `immersive_mode_entered` and `immersive_mode_exited`: `course_id`, `lesson_id`, `slide_index`, `trigger` (`control`, `rotation`, `back`, `escape`), and on exit a `seconds_in_mode` bucket.
- `immersive_control_used`: `control` (`play`, `pause`, `previous`, `next`, `replay_slide`, `autoplay_on`, `autoplay_off`, `speed`, `captions`, `narration`, `slide_list`, `bookmark`, `lock`, `unlock`) and `via` (`button`, `double_tap`, `swipe`, `keyboard`).
- `lesson_region_selected`: `course_id`, `lesson_id`, `slide_index`, `method` (`two_finger`, `select_area`, `keyboard`), `area_bucket` and `regions_matched` bucket.
- `lesson_region_question_sent`: `course_id`, `lesson_id`, `slide_index`, `input` (`voice`, `text`), `has_region` and `grounding` (`regions`, `whole_slide`).
- `lesson_answer_panel_closed`: `course_id`, `lesson_id`, `outcome` (`completed`, `stopped`, `failed`), `saved_note` and `follow_ups` bucket.

Properties SHALL be opaque identifiers or closed enums. Events SHALL honor opt-out and exclude question text, note text, Slide text and region descriptions.

#### Scenario: Opted-out Learner
- **WHEN** a Learner who opted out uses immersive playback or asks a region question
- **THEN** no immersive or region event is delivered

### Requirement: Anchored note analytics event
The API SHALL emit `lesson_anchored_note_created` once when a Quick note with a Lesson-moment anchor is first saved, with only `course_id`, `lesson_id`, `slide_index`, `entry_point` (`typed` or `voice`) with `source` `api` and `has_region`. It MUST NOT contain note text, transcript text, region coordinates or Slide text, MUST NOT be repeated when an offline queue replays the same note, and MUST be dropped for a Learner who has opted out.

#### Scenario: Anchored note created
- **WHEN** a Learner saves a typed note on a region
- **THEN** one `lesson_anchored_note_created` event with `entry_point` `typed` and `has_region` true is captured and no property holds note text

### Requirement: Immersive lesson and voice analytics catalogue
The analytics catalogue SHALL contain these events with only the listed properties, all identifiers or closed enums:
- `immersive_mode_entered` and `immersive_mode_exited`: `course_id`, `lesson_id`, `slide_index`, `trigger` (control, rotation, back, escape), `platform`, and on exit `seconds_in_mode` bucket.
- `immersive_control_used`: `control` (play, pause, previous, next, replay_slide, autoplay_on, autoplay_off, speed, captions, narration, slide_list, bookmark, lock, unlock), `via` (button, double_tap, swipe, keyboard).
- `lesson_region_selected`: `course_id`, `lesson_id`, `slide_index`, `method` (two_finger, select_area, keyboard), `area_bucket`, `regions_matched` bucket.
- `lesson_region_question_sent`: `course_id`, `lesson_id`, `slide_index`, `input` (voice, text), `has_region`, `grounding` (regions, whole_slide).
- `lesson_answer_panel_closed`: `course_id`, `lesson_id`, `outcome` (completed, stopped, failed), `saved_note`, `follow_ups` bucket.
- `lesson_anchored_note_created`: `course_id`, `lesson_id`, `slide_index`, `source` (typed, voice), `has_region`.
- `voice_recording_finished` and `voice_recording_cancelled`: `purpose` (tutor, note), `duration_bucket`.
- `voice_transcription_succeeded` and `voice_transcription_failed`: `purpose`, `duration_bucket`, `language`, `served_model`, `fallback_used`, and on failure `error_class`.
- `voice_transcript_used` and `voice_transcript_discarded`: `purpose`, `edited`.
Events MUST NOT contain audio, transcript text, question text, note text, Slide text or region descriptions, and MUST be dropped for a Learner who has opted out.

#### Scenario: Opted-out Learner
- **WHEN** a Learner who opted out of analytics asks a region question by voice
- **THEN** none of these events is sent

#### Scenario: No free text
- **WHEN** any event in this catalogue is captured
- **THEN** its properties contain only the listed identifiers and enum values

