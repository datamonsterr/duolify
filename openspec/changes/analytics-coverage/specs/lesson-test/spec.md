## MODIFIED Requirements

### Requirement: Test analytics events
The system SHALL emit `lesson_test_started`, `lesson_test_submitted`, `lesson_test_passed`, `lesson_test_answer_wrong`, `retry_variant_queued`, `retry_variant_served`, `retry_variant_answered` and `lesson_test_recap_shown` with the properties from the `analytics` spec. `lesson_test_passed` MUST carry `lesson_id`, `course_id`, `first_try_correct`, `question_count`, `variants_served` and `duration_ms`. Question text and answers MUST NOT be sent to PostHog.

#### Scenario: Test passed
- **WHEN** the last topic receives a correct answer
- **THEN** `lesson_test_passed` is emitted once with `first_try_correct`, `variants_served` and `duration_ms`

#### Scenario: Wrong answer
- **WHEN** a Learner answers a Question wrongly
- **THEN** `lesson_test_answer_wrong` is emitted with `topic_id`, `is_variant` and `attempt_for_topic`, and `retry_variant_queued` follows
