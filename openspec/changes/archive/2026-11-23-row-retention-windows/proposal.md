# Row retention windows

## Why
`lesson_playback_sync_events` and `lesson_test_answer_operations` only exist to make client retries idempotent, yet grow without bound. The performance audit proposed windows; the owner delegated the decision: sync events 30 days, answer operations 7 days.

## What Changes
- A worker River job `row_retention` (daily, unique per period) deletes rows older than the window, in batches of 5,000 with `FOR UPDATE SKIP LOCKED`, looping until a batch comes back short.
- Windows, cadence and batch size are env-configurable: `PLAYBACK_SYNC_EVENT_RETENTION` (30d), `LESSON_TEST_ANSWER_OPERATION_RETENTION` (7d, never below `LESSON_TEST_SESSION_TTL`), `ROW_RETENTION_SWEEP_INTERVAL` (24h), `ROW_RETENTION_BATCH_SIZE` (5000).
- Migration adds `created_at` indexes on both tables.

## Impact
`services/api` (`internal/retention`, `internal/worker`, `cmd/api`, one migration), `.env.example`, `docs/data-inventory.md`. No proto, client or engine change. Out of scope: other tables, backup retention, pricing.

## Data backup, analytics, model reliability
- Backup: rows are deleted from live Postgres only; dumps keep their 14-day life, restore compares unchanged tables. No new column, so `internal/backup/anonymize.go` needs no rule. Account erasure still cascades.
- Analytics: no learner event. The purge is an operational worker log line with counts only (no learner id); the analytics spec adds no event for it.
- No AI feature is touched: no eval or fallback change. Reliability: the job is idempotent, retried by River (5 attempts), safe with several workers, and a failure leaves rows for the next run.
