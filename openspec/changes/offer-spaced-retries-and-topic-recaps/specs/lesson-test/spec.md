## MODIFIED Requirements

### Requirement: Grading and pass rule
The API SHALL grade every answer server-side. The Learner MUST pass the Lesson test only by answering every topic correctly: each topic needs one correct answer, either to its original Question or to one of its Retry variants. A pass SHALL be the only event that marks the Lesson completed. The test MUST NOT complete until the unresolved-topic and variant obligation set is completely empty.

#### Scenario: Pass
- **WHEN** every topic of the Lesson test has a correct answer and no retry variants remain in queue
- **THEN** the Lesson stub status is marked completed, the completion is committed to the outbox once, and the next Lesson is marked ready_to_generate

#### Scenario: Wrong answer
- **WHEN** a Learner answers a Question wrongly
- **THEN** `lesson_test_answer_wrong` is emitted with `topic_id`, `is_variant` and `attempt_for_topic`, and a Retry variant is queued

### Requirement: Retry variants
After a wrong answer the API SHALL queue a Retry variant for the missed topic. A Retry variant MUST be a new Question on the same `topic_id` whose prompt and choices are paraphrased: the prompt wording MUST differ, the choices MUST be reworded, and the choice order MUST differ from the missed Question and from every earlier variant of that topic. The meaning and the correct answer MUST stay the same. A variant MUST be served later in the same Lesson test, after at least `min_gap` other Questions where enough remain (default 2, config), and MUST NOT be the next Question unless it is the only one left.

#### Scenario: Spaced variant insertion
- **WHEN** a Learner answers Question Q on topic T wrongly and at least `min_gap` questions remain
- **THEN** a Retry variant of T is inserted into the queue after at least `min_gap` other questions

#### Scenario: Only question left
- **WHEN** a Learner answers Question Q on topic T wrongly and no other questions remain in the queue
- **THEN** the Retry variant of T is queued next as the only remaining question

### Requirement: Variant limits and recap
The API MUST allow at most `max_variants_per_topic` (default 3, config) consecutive wrong answers on a topic before offering a recap. After that limit the client MUST offer a recap Slide of the Slides that teach the topic, and the Learner MUST still answer a further variant correctly to pass. The recap MUST NOT mark the topic correct. A single test session MUST NOT serve more than `max_variants_per_test` (default 12, config) variants without the client offering the Learner to leave and resume later.

#### Scenario: Topic recap limit reached
- **WHEN** a Learner misses a topic's original Question and `max_variants_per_topic` variants in a row
- **THEN** the client offers a taught-slide recap, viewing the recap emits `lesson_test_recap_shown`, and after the recap a further variant remains queued

#### Scenario: Leave and resume limit reached
- **WHEN** a single test session serves `max_variants_per_test` variants
- **THEN** the client offers the Learner to leave and resume later without resolving the pending topics
