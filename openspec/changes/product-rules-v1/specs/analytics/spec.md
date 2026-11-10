## MODIFIED Requirements

### Requirement: Course and lesson events
The catalog MUST include: `course_preview_requested` (topic_length, has_profile), `course_preview_viewed` (course preview id, duration_ms), `course_outline_generated` (course_id, unit_count, lesson_count, provider, model, latency_ms), `course_enrolled` (course_id), `course_generation_failed` (stage, error_code), `lesson_generation_requested` (course_id, lesson_id), `lesson_generated` (lesson_id, slide_count, provider, model, latency_ms), `lesson_started`, `slide_viewed` (index, watch_ms, offline), `slide_replayed`, `slide_paused`, `lesson_completed` (xp, duration_ms), `lesson_player_error` (error_code, slide_id), `lesson_test_started`, `lesson_test_submitted` (topic_id, correct, is_variant), `lesson_test_answer_wrong` (topic_id, is_variant, attempt_for_topic), `retry_variant_queued` (topic_id), `retry_variant_served` (topic_id, source: pool or on_demand), `retry_variant_answered` (topic_id, correct), `retry_variant_generation_failed` (topic_id, error_code), `retry_variant_pool_exhausted` (topic_id, error_code; no provider failure implied), `lesson_test_recap_shown` (topic_id) and `lesson_test_passed` (first_try_correct, question_count, variants_served, duration_ms).

#### Scenario: Catalog check
- **WHEN** a feature emits an event
- **THEN** its name and properties appear in the catalog or the change adds them

### Requirement: Credits and AI feature events
The catalog MUST include: `credits_reserved`, `credits_settled`, `credits_released` (feature, amount), `credits_insufficient` (feature, balance, estimate), `credits_upsell_shown` (feature, next_daily_grant_at), `credits_purchased` (amount, currency, price), `credits_purchase_failed` (reason), `daily_credits_granted` (amount), `daily_credits_expired` (amount), `tutor_message_sent` (has_lesson_context), `tutor_reply_completed`, `tutor_reply_stopped`, `tutor_reply_failed` (error_code), `quick_note_created` (source, length), `quick_note_enriched`, `quick_note_enrichment_failed`, `quick_note_reviewed`, `quick_note_deleted`, `bookmark_added`, `bookmark_removed`, `bookmark_opened` (target_type) and `learner_profile_updated` (fields_changed). The catalog MUST NOT contain `reward_credits_granted`.

#### Scenario: Purchase event
- **WHEN** a webhook confirms a purchase
- **THEN** `credits_purchased` is emitted once with `amount`, `currency` and `price`

## ADDED Requirements

### Requirement: Narration events
The catalog MUST include: `narration_generated` (lesson_id, slide_id, characters, voice_id, model, latency_ms), `narration_cache_hit` (lesson_id, slide_id), `narration_failed` (lesson_id, slide_id, error_code, retries), `narration_played` (lesson_id, slide_id, speed), `narration_muted` (lesson_id, muted), `narration_speed_changed` (lesson_id, speed) and `captions_toggled` (lesson_id, enabled). Narration text MUST NOT be sent to PostHog.

#### Scenario: Cache hit
- **WHEN** narration audio for an identical hash already exists
- **THEN** `narration_cache_hit` is emitted and no `narration_generated` event follows

Lesson test lifecycle events are emitted by the committing API transaction only. `duration_ms` comes from the session creation time, `score` is the fraction of original questions answered correctly on their first submission, and `app_version` is the validated client build version or `unknown`. Retry delivery uses `variant_source` to distinguish pool reuse from the common event `source=api`. Learner prompts, choices, explanations and selected answer values MUST NOT be sent.
