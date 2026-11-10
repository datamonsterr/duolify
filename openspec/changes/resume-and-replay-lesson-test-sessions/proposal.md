# Resume and replay lesson test sessions

## Why
Learners study on multiple devices, encounter transient disconnections, and take breaks mid-test. Ticket E4-04 establishes durable session resumption, offline answer replay idempotency, and multi-device concurrency safety. Tests stay resumable across devices and worker restarts with authoritative server state. Answers submitted offline are buffered locally and replayed using stable operation keys, preventing duplicate answers, unserved key leaks, and optimistic completion grants. Abandoned sessions exceeding the configured 7-day TTL restart with newly ordered questions without granting premature completion.

## What Changes
- Enforce server-authoritative session persistence in Postgres with configurable session TTL (`LESSON_TEST_SESSION_TTL`, default 7 days / 168h).
- Resuming mid-test restores the exact question queue, resolved topic set, and variant obligations without re-emitting `lesson_test_started`.
- Sessions older than the TTL are marked `abandoned` on access, initiating a fresh session with original questions in a newly ordered sequence without granting completion.
- Support idempotent answer submission via `operation_key` stored in `lesson_test_answer_operations`, preventing duplicate answers and double completion awards.
- Reject foreign operation keys and answers to unserved questions without revealing answer keys or explanations.
- Guarantee that duplicate or replayed final answers record exactly one completion in `lesson_completion_outbox` and emit `lesson_test_passed` only once.
- Update client `LessonTest` to queue offline answers, display canvas-compliant "Offline variant" / "Waiting to sync" states matching S10-Test and S12-Finish, and replay queued answers idempotently upon reconnection.
- Cover `lesson_test_answer_operations` in account deletion, data export, nightly backups, and privacy documentation.

## Impact
- API `services/api/internal/lessontest` session lifecycle and grading handlers.
- Database schema migration for `lesson_test_answer_operations` with sqlc generation.
- Client `apps/client/src/features/lessontest` offline buffering, replay sync, and canvas states.
- System analytics deduplication and privacy coverage.
