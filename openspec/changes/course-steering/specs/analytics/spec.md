## ADDED Requirements

### Requirement: Private Course steering event catalogue
The API SHALL deliver steering events through its existing consent-aware PostHog wrapper, with a UUID Learner distinct id and `source`, `platform`, `app_version` common properties. `course_instruction_added` SHALL contain `course_id`, closed `scope`, `length_bucket` (`1_100`, `101_250`, `251_500`) and `is_course_copy`; `course_instruction_rejected` SHALL contain `course_id` and a closed `reason_code`; `course_instruction_applied` SHALL contain `course_id` and `lesson_id`. Platform screening SHALL report `$ai_generation` with `feature=steer_screen`, provider/model, token usage, latency, fallback and success flags. No event SHALL contain instruction text, prompts or Course titles. Opt-out SHALL suppress delivery.
The existing `ui_sheet_opened` event SHALL accept `sheet_id=course_steering` for the instruction panel, with the same common properties and privacy rules.

#### Scenario: Safe instruction saved
- **WHEN** a Learner saves an allowed next-lesson instruction
- **THEN** PostHog receives `course_instruction_added` with its length bucket, never its text

#### Scenario: Analytics opt-out
- **WHEN** a Learner has opted out of analytics
- **THEN** steering and screening events are not delivered to PostHog
