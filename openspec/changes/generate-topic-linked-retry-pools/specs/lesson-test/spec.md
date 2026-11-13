## MODIFIED Requirements

### Requirement: Test content
Each generated Lesson SHALL include a Lesson test of multiple-choice Questions, each with a `topic_id`, a prompt, choices, one correct answer and an explanation. Every Question MUST belong to exactly one topic, and every topic MUST be taught by at least one Slide of the Lesson. The Learner MUST NOT receive `answer_index` or the explanation before submitting an answer to that Question.

#### Scenario: Test loaded
- **WHEN** the client fetches a Lesson test
- **THEN** Questions include prompt and choices but not the correct answer or explanation

### Requirement: Grading and pass rule
The API SHALL grade every answer server-side. The Learner MUST pass the Lesson test only by answering every topic correctly: each topic needs one correct answer, either to its original Question or to one of its Retry variants. A pass SHALL be the only event that marks the Lesson completed.

#### Scenario: Pass
- **WHEN** every topic of the Lesson test has a correct answer
- **THEN** the Lesson stub status is marked completed, the completion is committed to the outbox once, and the next Lesson is marked ready_to_generate

#### Scenario: Wrong answer
- **WHEN** a Learner answers a Question wrongly
- **THEN** `lesson_test_answer_wrong` is emitted with `topic_id`, `is_variant` and `attempt_for_topic`, and a Retry variant is queued

## ADDED Requirements

### Requirement: Retry variants
After a wrong answer the API SHALL queue a Retry variant for the missed topic. A Retry variant MUST be a new Question on the same `topic_id` whose prompt and choices are paraphrased: the prompt wording MUST differ, the choices MUST be reworded, and the choice order MUST differ from the missed Question and from every earlier variant of that topic. The meaning and the correct answer MUST stay the same. A variant MUST be served later in the same Lesson test, after at least `min_gap` other Questions where enough remain (default 2, config), and MUST NOT be the next Question unless it is the only one left.

#### Scenario: Missed question gets a variant
- **WHEN** a Learner answers Question Q on topic T wrongly
- **THEN** a Retry variant of T is queued and appears later in the same test with different wording and a different choice order

### Requirement: Variant generation and pools
The Engine SHALL generate a pool of 2 Retry variants for every topic together with the Lesson, so that the first corrections need no extra provider call or charge. Private pools MUST be stored in Postgres and validated for non-duplicates and taught-topic coverage. When a pool is exhausted, the pool is reused reordered for free per owner decision ADR 0011 / E4-06.

#### Scenario: Pool serves variant
- **WHEN** a Learner misses a Question and the topic pool has an unused variant
- **THEN** the variant is served from the pre-generated pool without extra charge
