## MODIFIED Requirements

### Requirement: Test content
Each generated Lesson SHALL include a Lesson test of multiple-choice Questions, each with a `topic_id`, a prompt, choices, one correct answer and an explanation. Every Question MUST belong to exactly one topic, and every topic MUST be taught by at least one Slide of the Lesson. The Learner MUST NOT receive `answer_index` or the explanation before submitting an answer to that Question.

#### Scenario: Test loaded
- **WHEN** the client fetches a Lesson test
- **THEN** Questions include prompt and choices but not the correct answer or explanation

### Requirement: Grading and pass rule
The API SHALL grade every answer server-side. The Learner MUST pass the Lesson test only by answering every topic correctly: each topic needs one correct answer, either to its original Question or to one of its Retry variants. A pass SHALL be the only event that marks the Lesson completed. There MUST be no percentage threshold.

#### Scenario: Pass
- **WHEN** every topic of the Lesson test has a correct answer
- **THEN** the Lesson is completed, XP is awarded, and generation of the next Lesson is enqueued

#### Scenario: Test not finished
- **WHEN** at least one topic has no correct answer yet
- **THEN** the Lesson stays incomplete and the next Lesson is not generated

### Requirement: Test analytics events
The system SHALL emit `lesson_test_started`, `lesson_test_submitted`, `lesson_test_passed`, `lesson_test_answer_wrong`, `retry_variant_queued`, `retry_variant_served`, `retry_variant_answered`, `retry_variant_generation_failed` and `lesson_test_recap_shown` with the properties from the `analytics` spec. `lesson_test_passed` MUST carry `lesson_id`, `course_id`, `first_try_correct`, `question_count`, `variants_served` and `duration_ms`. Question text and answers MUST NOT be sent to PostHog.

#### Scenario: Test passed
- **WHEN** the last topic receives a correct answer
- **THEN** `lesson_test_passed` is emitted once with `first_try_correct`, `variants_served` and `duration_ms`

#### Scenario: Wrong answer
- **WHEN** a Learner answers a Question wrongly
- **THEN** `lesson_test_answer_wrong` is emitted with `topic_id`, `is_variant` and `attempt_for_topic`, and `retry_variant_queued` follows

### Requirement: Test data backup and retention
Test sessions, answers (including variant answers), first-try results and variant Questions MUST be stored in Postgres, covered by the nightly dump, and kept for the life of the account. Account deletion MUST remove them within 30 days.

#### Scenario: Restore
- **WHEN** the restore drill runs
- **THEN** test sessions, answers and completion state match the dump

### Requirement: Test quality and reliability
Lesson tests and Retry variants MUST have Golden set cases (tasks `lesson_test` and `lesson_test_variant`). Deterministic checks for every Question and variant: one correct choice, no duplicate choices, answer index in range, explanation present, `topic_id` set. Deterministic checks for every variant: prompt text differs from the source Question, choice order differs from the source, the correct answer is at a different index or the choice set is reordered, the variant is not a duplicate of an earlier variant of the same topic, and the correct answer text, after normalization, is semantically equal to the source answer. An LLM judge MUST score each variant against the rubric for same meaning, same topic, same correct answer and no new facts, and MUST meet `evals/thresholds.toml`. The Engine MUST apply the timeout, provider fallback and output validation of `ai-reliability` to variant generation.

#### Scenario: Malformed question
- **WHEN** the Engine returns a Question with `answer_index` out of range
- **THEN** the API rejects the Lesson and triggers retry or fallback

#### Scenario: Variant is not a paraphrase
- **WHEN** the Engine returns a variant with the same prompt wording as the source Question
- **THEN** the API rejects the variant and requests another, and the eval check fails on such output

#### Scenario: Variant changes the answer
- **WHEN** the judge finds that a variant has a different correct answer than its source Question
- **THEN** the golden case fails

## ADDED Requirements

### Requirement: Immediate correction
When a Learner answers a Question wrongly, the client MUST show the correct answer and a short explanation immediately, before the next Question, and the Learner MUST acknowledge it to continue. The API MUST reveal `answer_index` and the explanation only in the response to a submitted answer.

#### Scenario: Wrong answer
- **WHEN** a Learner submits a wrong choice
- **THEN** the response contains the correct answer and the explanation and the client shows them before moving on

#### Scenario: Correct answer
- **WHEN** a Learner submits the correct choice
- **THEN** the Question is marked correct for its topic and the client moves on

### Requirement: Retry variants
After a wrong answer the API SHALL queue a Retry variant for the missed topic. A Retry variant MUST be a new Question on the same `topic_id` whose prompt and choices are paraphrased: the prompt wording MUST differ, the choices MUST be reworded, and the choice order MUST differ from the missed Question and from every earlier variant of that topic. The meaning and the correct answer MUST stay the same. A variant MUST be served later in the same Lesson test, after at least `min_gap` other Questions where enough remain (default 2, config), and MUST NOT be the next Question unless it is the only one left.

#### Scenario: Missed question gets a variant
- **WHEN** a Learner answers Question Q on topic T wrongly
- **THEN** a Retry variant of T is queued and appears later in the same test with different wording and a different choice order

#### Scenario: Variant answered wrongly
- **WHEN** a Learner answers a Retry variant wrongly
- **THEN** the correction is shown and a further variant of the same topic is queued, subject to the variant limits

### Requirement: Variant limits and recap
The API MUST allow at most `max_variants_per_topic` (default 3, config) consecutive wrong answers on a topic before offering a recap. After that limit the client MUST offer a recap Slide of the Slides that teach the topic, and the Learner MUST still answer a further variant correctly to pass. The recap MUST NOT mark the topic correct. A single test session MUST NOT serve more than `max_variants_per_test` (default 12, config) variants without the client offering the Learner to leave and resume later.

#### Scenario: Limit reached
- **WHEN** a Learner misses a topic's original Question and `max_variants_per_topic` variants in a row
- **THEN** the client offers a recap Slide, and after the recap one more variant of that topic is queued

#### Scenario: Recap does not pass
- **WHEN** a Learner views the recap and skips the next variant
- **THEN** the topic stays unanswered and the test cannot complete

### Requirement: Variant generation
The Engine SHALL generate a pool of `variants_per_topic` (default 2, config) Retry variants for every topic together with the Lesson, so that the first corrections need no extra call. When the pool for a topic is exhausted, the API MUST request more from the Engine on demand through the variant RPC, passing the source Question and all earlier variants of the topic so that none repeats. Variant generation MUST reserve and settle Credits per the `credits` spec; the pool generated with the Lesson is part of the Lesson generation charge. If on-demand generation fails or Credits are insufficient, the API MUST serve the source Question again with its choices reordered instead of blocking the test, and MUST emit `retry_variant_generation_failed`.

#### Scenario: Pool serves the first variant
- **WHEN** a Learner misses a Question and the topic pool has an unused variant
- **THEN** the variant is served without an Engine call

#### Scenario: Pool empty
- **WHEN** the pool of a topic is empty and the Learner misses again
- **THEN** the API calls the Engine for a new variant and none repeats an earlier wording

#### Scenario: Generation fails
- **WHEN** on-demand generation fails after retries
- **THEN** the source Question is served again with reordered choices and the test continues

### Requirement: Test session and resume
A Lesson test MUST run as one server-side session holding the Question queue, answers and topic state. A Learner SHALL be able to leave and resume the session. The session MUST be abandoned only after `test_session_ttl` (default 7 days, config); an abandoned session starts a new session with the original Questions in a new order.

#### Scenario: Resume
- **WHEN** a Learner leaves mid-test and returns the next day
- **THEN** the test resumes with the same queue, answers and queued variants

#### Scenario: Offline answers
- **WHEN** answers are replayed after reconnect
- **THEN** each answer is applied once and the queue matches the server state

## REMOVED Requirements

### Requirement: Retries
**Reason**: A Learner can no longer fail a Lesson test. Wrong answers produce Retry variants inside the same test.
**Migration**: Attempt rows become test sessions with per-answer rows; `first_try_correct` replaces the score.
