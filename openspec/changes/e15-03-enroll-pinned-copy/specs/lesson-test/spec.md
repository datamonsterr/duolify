## ADDED Requirements

### Requirement: Published lessons stay behind the Lesson test gate
A Lesson that arrived with a published Course copy MUST NOT be playable, testable or retrievable (player manifest, slides, Lesson test, Tutor retrieval) while its status is `locked`. Passing the predecessor's Lesson test MUST open it as `current` at zero Credits when the copy already holds its content, and as `ready_to_generate` otherwise.

#### Scenario: Locked reused lesson
- **WHEN** a Learner asks for the test or manifest of Lesson 2 before passing Lesson 1
- **THEN** the API answers not found and nothing is served

#### Scenario: Pass opens the reused lesson
- **WHEN** the Learner passes Lesson 1 and Lesson 2 is in the copy
- **THEN** Lesson 2 is `current`, the wallet is unchanged and `published_lesson_reused` is emitted once
