-- +goose Up
-- The retention sweep deletes by age; without these it scans each table on every batch.
CREATE INDEX IF NOT EXISTS idx_lesson_playback_sync_events_created_at ON lesson_playback_sync_events(created_at);
CREATE INDEX IF NOT EXISTS idx_lesson_test_answer_operations_created_at ON lesson_test_answer_operations(created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_lesson_test_answer_operations_created_at;
DROP INDEX IF EXISTS idx_lesson_playback_sync_events_created_at;
