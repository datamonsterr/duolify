-- +goose NO TRANSACTION
-- +goose Up
-- The retention sweep deletes by age; without these it scans each table on every batch.
-- lesson_playback_sync_events is written on every playback sync, so the indexes are built
-- CONCURRENTLY (hence NO TRANSACTION). IF NOT EXISTS keeps the file re-runnable and a no-op where
-- the plain form was already applied. A failed concurrent build leaves an INVALID index: drop it
-- and re-run.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_lesson_playback_sync_events_created_at ON lesson_playback_sync_events(created_at);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_lesson_test_answer_operations_created_at ON lesson_test_answer_operations(created_at);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_lesson_test_answer_operations_created_at;
DROP INDEX CONCURRENTLY IF EXISTS idx_lesson_playback_sync_events_created_at;
