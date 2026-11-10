-- +goose Up
ALTER TABLE quick_notes ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;
ALTER TABLE quick_notes ADD COLUMN IF NOT EXISTS reviewed_at timestamptz;
CREATE INDEX IF NOT EXISTS quick_notes_learner_reviewed ON quick_notes(learner_id, reviewed);

-- +goose Down
DROP INDEX IF EXISTS quick_notes_learner_reviewed;
ALTER TABLE quick_notes DROP COLUMN IF EXISTS reviewed_at;
ALTER TABLE quick_notes DROP COLUMN IF EXISTS version;
