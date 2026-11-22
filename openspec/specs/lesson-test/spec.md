# lesson-test Specification

## Purpose
End every Lesson with a short Lesson test. Passing it completes the Lesson, awards XP and unlocks Gated generation of the next Lesson.

## Requirements

### Requirement: Test content
Each generated Lesson SHALL include a Lesson test of multiple-choice Questions, each with a `topic_id`, a prompt, choices, one correct answer and an explanation. Every Question MUST belong to exactly one topic, and every topic MUST be taught by at least one Slide of the Lesson. The Learner MUST NOT receive `answer_index` or the explanation before submitting an answer to that Question.

#### Scenario: Test loaded
- **WHEN** the client fetches a Lesson test
- **THEN** Questions include prompt and choices but not the correct answer or explanation

### Requirement: Grading and pass rule
The API SHALL grade every answer server-side. The Learner MUST pass the Lesson test only by answering every topic correctly: each topic needs one correct answer, either to its original Question or to one of its Retry variants. A pass SHALL be the only event that marks the Lesson completed. The test MUST NOT complete until the unresolved-topic and variant obligation set is completely empty.

#### Scenario: Pass
- **WHEN** every topic of the Lesson test has a correct answer and no retry variants remain in queue
- **THEN** the Lesson stub status is marked completed, the completion is committed to the outbox once, and the next Lesson is marked ready_to_generate

#### Scenario: Fail
- **WHEN** a Learner scores below the threshold
- **THEN** the Lesson stays incomplete, explanations are shown and the Learner may retry
- **AND** the next Lesson is not generated

#### Scenario: Test not finished
- **WHEN** at least one topic has no correct answer yet
- **THEN** the Lesson stays incomplete and the next Lesson is not generated

#### Scenario: Wrong answer
- **WHEN** a Learner answers a Question wrongly
- **THEN** `lesson_test_answer_wrong` is emitted with `topic_id`, `is_variant` and `attempt_for_topic`, and a Retry variant is queued

### Requirement: Idempotent completion
Passing MUST be recorded once per Lesson. A duplicate submission, such as an offline replay of the final answer, MUST NOT award XP or enqueue generation twice.

#### Scenario: Duplicate pass submission
- **WHEN** the same passing attempt or duplicate final answer is sent twice
- **THEN** exactly one completion is recorded, `lesson_test_passed` is emitted once, and duplicate XP/generation are prevented

#### Scenario: Exact queue entries and correction replay
- **WHEN** a learner submits a topic, prompt, position alias, source question ID or an already consumed queue-item ID after receiving a correction
- **THEN** the API returns 400 without revealing another answer or resolving the original question
- **AND** only an exact served retry entry may resolve that original question

#### Scenario: Every original question resolves independently
- **WHEN** existing content contains more than one question for a topic
- **THEN** a correct answer resolves only its original question and passing requires a correct resolution for every original question
- **AND** newly generated lessons with duplicate question topics are rejected

#### Scenario: Concurrent start and completed replay
- **WHEN** multiple devices open the same lesson test or reopen it after passing
- **THEN** they share one in-progress session or the recorded passed session, respectively
- **AND** the original completion summary is insert-only and a pass event is emitted only when the completion outbox entry is first inserted

### Requirement: Test result feeds history
The API SHALL persist a short summary of each completed Lesson and its test result with weak topics (missed questions). These summaries MUST be sent as `history` to the Engine for subsequent Lesson generation, oldest first.

#### Scenario: Next lesson generation
- **WHEN** Lesson N+1 is generated
- **THEN** the request history contains summaries of completed Lessons, oldest first, including weak topics from missed Questions

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

#### Scenario: Spaced variant insertion
- **WHEN** a Learner answers Question Q on topic T wrongly and at least `min_gap` questions remain
- **THEN** a Retry variant of T is inserted into the queue after at least `min_gap` other questions

#### Scenario: Only question left
- **WHEN** a Learner answers Question Q on topic T wrongly and no other questions remain in the queue
- **THEN** the Retry variant of T is queued next as the only remaining question

### Requirement: Variant limits and recap
The API MUST allow at most `max_variants_per_topic` (default 3, config) consecutive wrong answers on a topic before offering a recap. After that limit the client MUST offer a recap Slide of the Slides that teach the topic, and the Learner MUST still answer a further variant correctly to pass. The recap MUST NOT mark the topic correct. A single test session MUST NOT serve more than `max_variants_per_test` (default 12, config) variants without the client offering the Learner to leave and resume later.

#### Scenario: Limit reached
- **WHEN** a Learner misses a topic's original Question and `max_variants_per_topic` variants in a row
- **THEN** the client offers a recap Slide, and after the recap one more variant of that topic is queued

#### Scenario: Recap does not pass
- **WHEN** a Learner views the recap and skips the next variant
- **THEN** the topic stays unanswered and the test cannot complete

#### Scenario: Topic recap limit reached
- **WHEN** a Learner misses a topic's original Question and `max_variants_per_topic` variants in a row
- **THEN** the client offers a taught-slide recap, viewing the recap emits `lesson_test_recap_shown`, and after the recap a further variant remains queued

#### Scenario: Leave and resume limit reached
- **WHEN** a single test session serves `max_variants_per_test` variants
- **THEN** the client offers the Learner to leave and resume later without resolving the pending topics

### Requirement: Test session and resume
A Lesson test MUST run as one server-side session holding the Question queue, answers and topic state. A Learner SHALL be able to leave and resume the session. The session MUST be abandoned only after `test_session_ttl` (default 7 days, config); an abandoned session starts a new session with the original Questions in a new order without granting completion.

#### Scenario: Resume
- **WHEN** a Learner leaves mid-test and returns within the session TTL
- **THEN** the test resumes with the same queue, answers and queued variants without re-emitting `lesson_test_started`

#### Scenario: Offline answers
- **WHEN** answers are replayed after reconnect
- **THEN** each answer is applied once using its operation key bound to learner, session, lesson, opaque queue-item ID and selected index; the key is checked under the submission lock, and unserved keys are rejected without exposing answers or explanations, and the queue matches server state

#### Scenario: Session abandoned after TTL
- **WHEN** a Learner returns to a test session after `test_session_ttl` has elapsed
- **THEN** the old session is marked abandoned, a new session starts with the original questions in a new order, and completion is not granted

### Requirement: Steering cannot weaken the Lesson test
Course instructions MUST NOT change the pass rule, grading, number of required correct answers, Retry variant behavior or correctness checks of a Lesson test. A generated Lesson test MUST still contain questions that cover the Lesson and pass validation regardless of instructions. A Lesson test already generated MUST NOT change when instructions change, and Retry variants MUST use the instructions snapshot stored on the Lesson.

#### Scenario: Instruction asks to skip the test
- **WHEN** an instruction says "no quiz" or "make all answers A"
- **THEN** the Lesson still has a valid Lesson test that must be passed with every answer correct

#### Scenario: Instruction changed after generation
- **WHEN** the Learner deletes an instruction after Lesson 3 was generated
- **THEN** the Lesson 3 test and its Retry variants are unchanged

### Requirement: Character reactions are presentation only
Lesson tests SHALL show supportive Character reactions using the Lesson's pinned Cast and approved expressions, selected deterministically from the API's authoritative answer result. Reactions MUST NOT grade answers, expose correct answers before submission, change the all-correct pass rule or required correct answers, suppress corrections/Retry variants, award XP or bypass Gated generation. Reactions MUST use deterministic selection without a runtime LLM call. Persona requests to accept wrong answers MUST have no effect.

#### Scenario: Incorrect answer
- **WHEN** the API grades an answer incorrect
- **THEN** a supportive `thinking` or `sad` reaction accompanies the normal correction and queued Retry variant, and the Lesson remains incomplete

#### Scenario: Successful completion
- **WHEN** every required question and Retry variant has been answered correctly
- **THEN** a `celebrate` reaction accompanies the authoritative pass and existing idempotent completion flow

#### Scenario: Reaction unavailable
- **WHEN** a Character asset cannot render or a reaction payload is invalid
- **THEN** a safe default or no animation is shown while corrections, retries and grading continue normally

### Requirement: Character test analytics events
The client SHALL emit PostHog `character_reaction_shown` (speaker_key, expression, reaction_trigger, fallback_used) alongside existing test events, with properties from the `analytics` delta. `reaction_trigger` MUST be a bounded category (`answer_correct`, `answer_incorrect`, `test_passed`), never answer text or an answer index. Events MUST respect opt-out and exclude questions, answers and persona content.

#### Scenario: Supportive error reaction observed
- **WHEN** an incorrect-answer reaction is shown
- **THEN** `character_reaction_shown` reports the trigger and expression without the submitted or correct answer

### Requirement: Character reaction data backup and retention
Pinned Cast references and any stored approved reaction metadata MUST live with Lesson/test records in Postgres and be included in nightly dumps. Referenced S3 rig assets MUST use versioning where supported and nightly protected asset backup/restore under `custom-characters`. Rendered animation frames and analytics MUST NOT be the only copy of grading state; no new answer store is required. Reaction metadata MUST follow Lesson/test/account deletion within 30 days, except shared assets retained under E15 references.

#### Scenario: Restore test and reactions
- **WHEN** Lesson/test data and rig assets are restored
- **THEN** reactions can render from the pinned Cast and authoritative attempts, scores and Retry variant state are unchanged

### Requirement: Character reaction model testing and reliability
Golden set `character_reactions` and `character_injection` cases MUST prove persona text cannot change grading, pass rules, retry queues or answer privacy, with deterministic immutability score 1.00 and any grading change a critical failure. Cases MUST meet `custom-characters` injection thresholds in `evals/thresholds.toml` and pass `mise run engine:eval`. Runtime reactions MUST be deterministic with no model timeout or added AI spend; invalid Character data MUST use the Default cast or a static neutral reaction. Test generation MUST retain existing provider fallback, timeouts and cost caps.

#### Scenario: Persona says every answer is correct
- **WHEN** a Golden set Character persona attempts to override answer correctness
- **THEN** the test still requires every answer/Retry variant correct and any deviation fails the eval gate

### Requirement: Gated generation and reservation
Only eligible lessons SHALL trigger lesson generation. For any lesson beyond the first, the predecessor Lesson MUST have a recorded authoritative pass; missing predecessor pass MUST refuse before any Engine call. Passing Lesson N enqueues only Lesson N+1. A failed or unaffordable next generation MUST NOT undo an already completed Lesson.

#### Scenario: Terminal failure releases credits
- **WHEN** generation fails after all attempts
- **THEN** the job is marked failed, reserved credits are released, and the lesson stays ungenerated

#### Scenario: Predecessor pass required
- **WHEN** preparation of Lesson N+1 is requested and Lesson N is not completed
- **THEN** the API refuses with a 409 conflict error and no Engine call or charge occurs

#### Scenario: Next generation failure leaves completion intact
- **WHEN** preparation of Lesson N+1 fails or credits are insufficient
- **THEN** reserved credits are released and Lesson N remains completed

#### Scenario: Duplicate pass submission
- **WHEN** the same passing attempt is sent twice
- **THEN** the completion outbox exists once and no duplicate generation job or charge is created

### Requirement: Variant generation and pools
The Engine SHALL generate a pool of `variants_per_topic` Retry variants (default 2) for every topic together with the Lesson, so that the first corrections need no extra provider call or charge. See `Variant generation and exhausted pools` for configurable pool size and exhausted-pool behavior. Private pools MUST be stored in Postgres and validated for non-duplicates and taught-topic coverage. When a pool is exhausted, the pool is reused reordered for free per owner decision ADR 0011 / E4-06.

#### Scenario: Pool serves variant
- **WHEN** a Learner misses a Question and the topic pool has an unused variant
- **THEN** the variant is served from the pre-generated pool without extra charge

### Requirement: Variant generation and exhausted pools
The Engine SHALL generate a pool of `variants_per_topic` (default 2, config) Retry variants for every topic together with the Lesson, so that initial corrections require no additional network call. When a validated pool for a topic is exhausted, the API MUST reuse an approved pool item with reordered choices for free without an Engine or provider call, keeping tests usable at zero balance. A missing or invalid pool MUST return unavailable without consuming the answer or echoing its source Question; see review #161 and the ADR 0011 implementation clarification. The API MUST NOT generate Retry variants on demand at launch (ADR 0011, E4-06: no on-demand paid AI variants), so there is no runtime generation failure to report; `retry_variant_generation_failed` is not emitted by the API. Pool exhaustion MUST emit `retry_variant_pool_exhausted` with the topic ID and error code, and MUST emit `retry_variant_served` without learner text. The API MUST retain the lesson instruction snapshot, enforce recap limits, and never auto-pass any topic.

#### Scenario: Pool exhausted free reuse
- **WHEN** a Learner answers wrongly on topic T and all pre-generated variants for T have already been served
- **THEN** the API serves a recycled variant with reordered choices for free without an Engine call, records its snapshot, emits `retry_variant_pool_exhausted` and `retry_variant_served`, and does not block zero-balance learners

#### Scenario: Missing or invalid pool fallback
- **WHEN** a retry pool for topic T is missing or invalid
- **THEN** the API returns unavailable without consuming the answer, charging credits, serving the identical source prompt or resolving the topic

#### Scenario: Never auto-pass
- **WHEN** an exhausted pool variant is served
- **THEN** the learner must submit the correct answer to resolve the topic; viewing recaps or encountering errors does not mark the topic passed

### Requirement: Durable generation settlement
The API MUST persist validated generated lessons and settle delivered Credits in the same transaction. A private durable checkpoint MUST permit worker retries after persistence failure without another provider call. Terminal generation failures MUST release customer Credits using the owning learner and MUST retain cleanup intent when financial persistence fails. Checkpoints MUST join full-row backups, owner export and cascading deletion, and MUST be cleared on terminal state.

#### Scenario: Storage fails after provider success
- **WHEN** content persistence fails after a validated generation
- **THEN** no customer charge is committed and a worker retry commits the saved content and settlement atomically without another provider call

#### Scenario: Financial cleanup fails
- **WHEN** a failed generation cannot release its reservation because the ledger is unavailable
- **THEN** the worker returns an error and retries durable cleanup before marking the generation terminal

### Requirement: Preparation requires an unused eligible lesson
Preparation MUST require ready_to_generate, enforce one active generation per lesson in Postgres and enqueue the River job in the same transaction as generation creation and stub state.

#### Scenario: Current lesson preparation
- **WHEN** a learner tries to prepare an already current lesson
- **THEN** the API rejects the request without reserving credits or rewriting content

#### Scenario: Queue unavailable
- **WHEN** River enqueue fails
- **THEN** generation creation and the lesson state change roll back together

### Requirement: Private recap delivery
Recaps MUST filter slides by both lesson and authenticated learner.

#### Scenario: Other learner requests a recap
- **WHEN** a learner requests another learner's lesson topic recap
- **THEN** no personalized slide content is returned

### Requirement: Real bounded paraphrases
The Engine MUST use its real provider path for retry paraphrases, invoke the core variant validators and allow at most one repair within the original quote's remaining attempt, USD and deadline bounds. Missing topic IDs MUST be rejected rather than defaulted. Cosmetic prefixes and identical source prompts MUST NOT be accepted as paraphrases. Approved exhausted pools MAY be reused reordered for free. An unavailable initial pool MUST reject generation; a missing or corrupt stored pool MUST fail safely without consuming the answer, charging, auto-passing or echoing its source prompt.

#### Scenario: Invalid paraphrase twice
- **WHEN** both primary and repair produce invalid paraphrases
- **THEN** generation falls back only to an approved pool or returns unavailable with no delivered customer charge

#### Scenario: Golden reliability gate
- **WHEN** offline golden cases execute the production paraphrase processor with a mock provider
- **THEN** E10 critical checks enforce topic linkage, unchanged correct-answer meaning, different prompt wording and the two-attempt ceiling

#### Scenario: Safe analytics
- **WHEN** generation or retry events are emitted
- **THEN** only allowlisted event metadata is sent and no prompts, learner text or checkpoint content enters analytics

### Requirement: Published lessons stay behind the Lesson test gate
A Lesson that arrived with a published Course copy MUST NOT be playable, testable or retrievable (player manifest, slides, Lesson test, Tutor retrieval) while its status is `locked`. Passing the predecessor's Lesson test MUST open it as `current` at zero Credits when the copy already holds its content, and as `ready_to_generate` otherwise.

#### Scenario: Locked reused lesson
- **WHEN** a Learner asks for the test or manifest of Lesson 2 before passing Lesson 1
- **THEN** the API answers not found and nothing is served

#### Scenario: Pass opens the reused lesson
- **WHEN** the Learner passes Lesson 1 and Lesson 2 is in the copy
- **THEN** Lesson 2 is `current`, the wallet is unchanged and `published_lesson_reused` is emitted once

### Requirement: Index-friendly identifiers and quiet generation stream
The API SHALL compare identifier columns with typed parameters (`column = $n::uuid`) so primary-key and foreign-key indexes apply, SHALL validate identifiers at the handler boundary and answer a malformed identifier with a client error rather than a database error, and the lesson generation event stream SHALL poll with a backoff that resets on a state change while keeping its heartbeats and terminal events.

#### Scenario: Malformed lesson id
- **WHEN** a client requests a lesson, test, recap, preparation or generation route with an identifier that is not a UUID
- **THEN** the API returns 404 and runs no query against Postgres

#### Scenario: Idle generation stream
- **WHEN** a generation stays in one state for several seconds
- **THEN** the stream reads Postgres less often as time passes and still emits its heartbeat and the terminal event

### Requirement: Lesson test enabled by default
The Lesson test SHALL be enabled by default in every environment. `LESSON_TEST_TRACER_ENABLED` and `VITE_LESSON_TEST_TRACER_ENABLED` are explicit kill switches; only explicit `false` disables the feature. An unparseable API value MUST fail startup. The client SHALL prefer the server value from `/v1/features`.

#### Scenario: Flags unset
- **WHEN** the flags are unset
- **THEN** the Lesson test is enabled

#### Scenario: Kill switch set
- **WHEN** the API and client flags are explicitly false
- **THEN** Lesson test routes are unavailable and the client hides the feature

### Requirement: Answer operation retention
The worker SHALL delete `lesson_test_answer_operations` rows more than 7 days old (`LESSON_TEST_ANSWER_OPERATION_RETENTION`), never sooner than `LESSON_TEST_SESSION_TTL`, in batches and safely with several workers. These rows hold only idempotency keys and replay payloads; sessions, answers, first-try results and variants are NOT purged and keep their account-lifetime retention. After a key is purged a replayed answer MUST be rejected by the session state and MUST NOT be applied twice. Purge counts SHALL be an operational log only, with no learner identifier and no PostHog event.

#### Scenario: Old operation purged
- **WHEN** the sweep runs and an operation row is 8 days old and another is 6 days old
- **THEN** only the 8-day-old row is deleted and answers and scores are unchanged

#### Scenario: Window below the session TTL
- **WHEN** the configured window is shorter than `LESSON_TEST_SESSION_TTL`
- **THEN** the effective window equals the session TTL

#### Scenario: Replay after purge
- **WHEN** a client replays an answer whose operation key was purged
- **THEN** the answer is not applied a second time
