# lesson-test Specification

## Purpose
End every Lesson with a short Lesson test. Passing it completes the Lesson, awards XP and unlocks Gated generation of the next Lesson.

## Requirements

### Requirement: Test content
Each generated Lesson SHALL include a Lesson test of multiple-choice Questions, each with a prompt, choices, one correct answer and an explanation. The Learner MUST NOT receive `answer_index` before submitting.

#### Scenario: Test loaded
- **WHEN** the client fetches a Lesson test
- **THEN** Questions include prompt and choices but not the correct answer

### Requirement: Grading and pass rule
The API SHALL grade submissions server-side. A test MUST be passed when the score is at or above the pass threshold in config (default 80%). Only a pass SHALL mark the Lesson completed.

#### Scenario: Pass
- **WHEN** a Learner scores at or above the threshold
- **THEN** the Lesson is completed, XP is awarded, and generation of the next Lesson is enqueued

#### Scenario: Fail
- **WHEN** a Learner scores below the threshold
- **THEN** the Lesson stays incomplete, explanations are shown and the Learner may retry
- **AND** the next Lesson is not generated

### Requirement: Retries
A Learner SHALL be able to retry a failed test. Each retry MUST present the same Questions reordered. Attempts MUST be stored with score and timestamp.

#### Scenario: Retry
- **WHEN** a Learner retries
- **THEN** a new attempt row is created and the best attempt decides completion

### Requirement: Idempotent completion
Passing MUST be recorded once per Lesson. A duplicate submission, such as an offline replay, MUST NOT award XP or enqueue generation twice.

#### Scenario: Duplicate pass submission
- **WHEN** the same passing attempt is sent twice
- **THEN** XP and the generation job exist once

### Requirement: Test result feeds history
The API SHALL store a short summary of each completed Lesson and its test result. These summaries MUST be sent as `history` to the Engine for later generation and Tutor chat.

#### Scenario: Next lesson generation
- **WHEN** Lesson N+1 is generated
- **THEN** the request history contains summaries of completed Lessons, oldest first, including weak topics from missed Questions

### Requirement: Test analytics events
The system SHALL emit `lesson_test_started`, `lesson_test_submitted`, `lesson_test_passed` and `lesson_test_failed` with `lesson_id`, `course_id`, `score`, `attempt` and `duration_ms`.

#### Scenario: Test passed
- **WHEN** a test is passed
- **THEN** `lesson_test_passed` is emitted once with `score` and `attempt`

### Requirement: Test data backup and retention
Attempts, answers and scores MUST be stored in Postgres, covered by the nightly dump, and kept for the life of the account. Account deletion MUST remove them within 30 days.

#### Scenario: Restore
- **WHEN** the restore drill runs
- **THEN** attempts and completion state match the dump

### Requirement: Test quality and reliability
Lesson tests MUST have Golden set cases (task `lesson_test`) with deterministic checks: one correct choice, no duplicate choices, answer index in range, explanation present. An LLM judge MUST score question quality against the rubric and meet `evals/thresholds.toml`.

#### Scenario: Malformed question
- **WHEN** the Engine returns a Question with `answer_index` out of range
- **THEN** the API rejects the Lesson and triggers retry or fallback
