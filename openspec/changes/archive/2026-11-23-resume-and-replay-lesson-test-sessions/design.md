# Design: Resume and Replay Lesson Test Sessions

## Overview
This design covers session persistence, time-to-live expiration, offline answer replay idempotency, and multi-device concurrency control for lesson tests.

## Architecture

### 1. Session Persistence and TTL Expiry
- `lesson_test_sessions` holds the authoritative state: `question_queue`, `resolved_topics`, `first_try_correct`, `variants_served`, and `state` (`in_progress`, `passed`, `abandoned`).
- When a client queries `GET /v1/lessons/:lesson_id/test`:
  - Active session (`in_progress`) is checked against `LESSON_TEST_SESSION_TTL` (default 7 days).
  - If expired:
    - Session state transitions to `abandoned`.
    - A new session is initialized with the lesson's original questions in a cyclically shifted / permuted order (`ReorderQuestions`).
    - `lesson_test_started` is emitted for the new session.
  - If valid and active:
    - Active question queue, resolved topic count, and session metadata are returned.
    - `lesson_test_started` is NOT emitted on resume (authoritative deduplication).

### 2. Idempotent Offline Answer Replay & Operation Keys
- Clients generate a unique `operation_key` (`^[A-Za-z0-9_-]{1,100}$`) for each answer attempt.
- `lesson_test_answer_operations` stores processed operations with their JSON response payloads:
  - `operation_key` (PK)
  - `session_id` (FK)
  - `learner_id` (FK)
  - `lesson_id` (FK)
  - `question_id`, `choice_index`
  - `response_payload`
  - `created_at`
- When `POST /v1/lessons/:lesson_id/test/answer` is called:
  - If `operation_key` belongs to another learner -> HTTP 400 Bad Request.
  - If `operation_key` already exists for this learner -> return cached response payload immediately.
  - If unserved question key (question not in active queue) -> HTTP 400 Bad Request without leaking answer index or explanation.
  - In a serialized transaction (`FOR UPDATE` on session row):
    - Process grading and variant queuing.
    - Persist answer to `lesson_test_answers`.
    - If all topics are resolved and queue empty: transition session to `passed`, mark lesson stub `done`, enqueue atomic completion in `lesson_completion_outbox` (`ON CONFLICT DO NOTHING`), emit `lesson_test_passed` once.
    - Persist operation in `lesson_test_answer_operations`.

### 3. Client Offline Experience & Canvas States
- When offline or during network failures:
  - Answers are enqueued in client localStorage queue (`lessontest:sync-queue:<owner>:<lesson_id>`).
  - Active view presents the canvas S10-Test "Offline variant" banner:
    "Offline variant — X answers are waiting to be checked. You'll see results and XP when you reconnect."
  - When all questions are answered offline, S12-Finish presents "Waiting to sync" without optimistically unlocking XP or enqueuing lesson generation.
  - Upon reconnection, queued answers are sent sequentially to `/v1/lessons/:lesson_id/test/answer` with their preserved `operation_key`.
  - Once verified by the server, the interface transitions to completed state with awarded XP.

### 4. Data Privacy & Retention
- `lesson_test_answer_operations` is registered in `ClassifiedLearnerTables` as `erased`.
- Account data export includes operation rows for the requesting learner.
- Account deletion cascades removal.
- Nightly dump covers `lesson_test_answer_operations` alongside sessions and answers.
