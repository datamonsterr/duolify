## Why
About 240 API statements compared a uuid column cast to text with a text parameter (`id::text = $1`). The cast on the column prevents use of primary-key and foreign-key indexes, so each becomes a sequential scan as tables grow (courses, lesson slides, questions, sessions, credits). The lesson generation event stream also polled Postgres every 200 ms for the whole generation.

## What Changes
- Predicates bind the parameter as uuid and compare the native column (`id = $1::uuid`, `= ANY($1::uuid[])`); `::text` stays only in select lists and for genuinely text columns.
- Ids from the path, query or body are validated at the handler boundary; a malformed id gets a client error (404, or 409 on conflict-style routes), never a database error.
- The lesson generation event stream polls with the creation stream's backoff (200 ms doubling to 3 s, reset on a state change); heartbeats and terminal events are unchanged. The helper moves to `internal/sse` and is shared.
- A CI guard (`mise run ci:sql-casts`) rejects a cast column compared to a parameter in non-test Go and sqlc sources, with a short commented allowlist.
- An integration test EXPLAINs the real hot-path statements on a few thousand seeded learners and requires index scans.
- Narration orphan purge runs regardless of the narration feature flag, and the PostHog person deletion queue is purged from non-production restores.

## Impact
- Affected specs: lesson-test
- Affected code: services/api (courses, lessontest, sse, credits, player, characters, store, pricing, gamification, steering, backup, narration), mise tasks, CI
- No schema, proto or AI changes. Data backup is unchanged apart from the anonymizer purging one more table of learner ids in restores. Analytics events are unchanged. Model testing does not apply (no AI behaviour change).
