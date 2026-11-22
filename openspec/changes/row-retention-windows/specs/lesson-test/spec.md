## ADDED Requirements

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
