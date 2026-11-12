-- +goose Up
ALTER TABLE quick_notes ADD COLUMN IF NOT EXISTS raw_version integer NOT NULL DEFAULT 1;
CREATE INDEX IF NOT EXISTS quick_notes_pending_enrichment ON quick_notes(learner_id, created_at ASC) WHERE status = 'enrichment_pending';

-- +goose Down
DROP INDEX IF EXISTS quick_notes_pending_enrichment;
ALTER TABLE quick_notes DROP COLUMN IF EXISTS raw_version;
