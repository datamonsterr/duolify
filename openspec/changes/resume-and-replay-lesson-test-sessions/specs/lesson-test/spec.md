## MODIFIED Requirements

### Requirement: Test session and resume
A Lesson test MUST run as one server-side session holding the Question queue, answers and topic state. A Learner SHALL be able to leave and resume the session. The session MUST be abandoned only after `test_session_ttl` (default 7 days, config); an abandoned session starts a new session with the original Questions in a new order without granting completion.

#### Scenario: Resume
- **WHEN** a Learner leaves mid-test and returns within the session TTL
- **THEN** the test resumes with the same queue, answers and queued variants without re-emitting `lesson_test_started`

#### Scenario: Session abandoned after TTL
- **WHEN** a Learner returns to a test session after `test_session_ttl` has elapsed
- **THEN** the old session is marked abandoned, a new session starts with the original questions in a new order, and completion is not granted

#### Scenario: Offline answers
- **WHEN** answers are replayed after reconnect
- **THEN** each answer is applied once using its operation key, unserved keys are rejected without exposing answers or explanations, and the queue matches server state

### Requirement: Idempotent completion
Passing MUST be recorded once per Lesson. A duplicate submission, such as an offline replay of the final answer, MUST NOT award XP or enqueue generation twice.

#### Scenario: Duplicate pass submission
- **WHEN** the same passing attempt or duplicate final answer is sent twice
- **THEN** exactly one completion is recorded, `lesson_test_passed` is emitted once, and duplicate XP/generation are prevented
