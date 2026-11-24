## ADDED Requirements

### Requirement: Retention events
The client and API SHALL emit `guess_first_answered` {lesson_id, correct, skipped}, `explain_why_submitted` {lesson_id, coverage, skipped, input}, `free_recall_submitted` {lesson_id, coverage, skipped}, `confidence_tapped` {question_kind, level, correct}, `review_started` {due_count}, `review_completed` {due_count, answered, correct}, `teach_back_completed` {lesson_id, coverage} and `tutor_answer_guard_hit` {lesson_id}. Learner text, answers and question text MUST NOT be sent to PostHog.

#### Scenario: Explain why submitted
- **WHEN** a Learner submits an explanation by voice
- **THEN** `explain_why_submitted` carries coverage and `input: voice` and no text
