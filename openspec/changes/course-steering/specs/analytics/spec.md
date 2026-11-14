## ADDED Requirements

### Requirement: Private Course steering event catalogue
The API SHALL deliver steering events through its existing consent-aware PostHog wrapper, with a UUID Learner distinct id and `source`, `platform`, `app_version` common properties. `course_instruction_added` SHALL contain `course_id`, closed `scope`, `length_bucket` (`1_100`, `101_250`, `251_500`) and `is_course_copy`; `course_instruction_rejected` SHALL contain `course_id` and a closed `reason_code`; `course_instruction_applied` SHALL contain `course_id` and `lesson_id`. `course_instruction_edited` SHALL contain `course_id`, closed `scope`, `length_bucket` and closed `changed` (`text`, `scope`, `both`); `course_instruction_deleted` SHALL contain `course_id` and closed `scope`. Both are emitted only after the change commits and never carry instruction text. Platform screening SHALL report `$ai_generation` with `feature=steer_screen`, provider/model, token usage, latency, fallback and success flags. No event SHALL contain instruction text, prompts or Course titles. Opt-out SHALL suppress delivery.
The existing `ui_sheet_opened` event SHALL accept `sheet_id=course_steering` for the instruction panel, with the same common properties and privacy rules.

Re-plan emits `course_replan_quoted` with `course_id`, `credits`, `pricing_version`; `course_replan_proposed` with `course_id`, `proposal_id`, `lessons_added`, `lessons_removed`, `lessons_changed`; and `course_replan_failed` with `course_id` and closed `reason` (`quote_invalid`, `insufficient_credits`, `protected_lesson_changed`, `invalid_proposal`, `replan_unavailable`, `generation_failed`). `$ai_generation` accepts `feature=steer_replan`, provider/model, token counts, latency, fallback and success flags and the Pricing version. UUID and common properties, opt-out and text exclusion apply to all these events.

#### Scenario: Paid proposal delivered
- **WHEN** a valid remaining-outline proposal is stored and settled
- **THEN** PostHog receives `course_replan_proposed` and `$ai_generation` without outline or instruction text

#### Scenario: Edit and delete
- **WHEN** a Learner edits or deletes an instruction
- **THEN** PostHog receives `course_instruction_edited` (changed, scope, length bucket) or `course_instruction_deleted` (scope), never the text

#### Scenario: Safe instruction saved
- **WHEN** a Learner saves an allowed next-lesson instruction
- **THEN** PostHog receives `course_instruction_added` with its length bucket, never its text

#### Scenario: Analytics opt-out
- **WHEN** a Learner has opted out of analytics
- **THEN** steering and screening events are not delivered to PostHog
