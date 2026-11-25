## ADDED Requirements

### Requirement: Lesson quality pipeline events
The client and API SHALL emit `outline_clarify_shown` {question_count}, `outline_clarify_answered` {question_id, skipped}, `lesson_length_changed` {course_id, from, to}, `lesson_estimate_shown` {course_id, lesson_length, credits, surface} and `review_question_answered` {lesson_id, source_lesson_id, correct}. `$ai_generation` SHALL add `lesson_length`, `plan_present` and `quality_warnings` (codes). Question text, answers and clarification text MUST NOT be sent to PostHog.

#### Scenario: Clarification answered
- **WHEN** a Learner answers a clarifying question
- **THEN** `outline_clarify_answered` carries only the question id and skipped flag
