# Design

## What each row is for
- `lesson_playback_sync_events` (PK learner_id, action_key): dedupe key only. The effect is folded into `lesson_playback_positions` in the same transaction as the insert, so it is durable state before any purge. It feeds no XP, streak or completion (spec: sync does not award completion or XP). A replay of a purged key after 30 days is merged by `occurred_at` and cannot move the cursor backwards.
- `lesson_test_answer_operations` (PK operation_key): replay cache returning the stored response for a retried answer. Sessions are abandoned after `LESSON_TEST_SESSION_TTL` (7d, measured from creation), so an operation row older than the TTL belongs to an abandoned or passed session. A replayed answer after purge is rejected (`unserved question key`, or 410) and never applied twice. Answers, scores and attempts live in `lesson_test_answers`, which is not purged. The window is clamped to at least the session TTL so a longer configured TTL cannot outlive the keys.

## Spec check
`lesson-test` "Test data backup and retention" covers sessions, answers and variants for the account lifetime, not operation keys; `lesson-player` requires idempotent replays, which hold (see above). No guarantee in `openspec/specs/**` is broken, so the purge proceeds.

## Sweep
The cutoff is `created_at` (server clock), not the client `occurred_at`. Each batch is `DELETE ... WHERE ctid = ANY(ARRAY(SELECT ctid ... ORDER BY created_at LIMIT n FOR UPDATE SKIP LOCKED))`, repeated until fewer than n rows are deleted. Concurrent sweeps skip locked rows, so none waits, fails or double-deletes. River uniqueness per period keeps replicas to one run; `RunOnStart` repairs a worker that was down.
