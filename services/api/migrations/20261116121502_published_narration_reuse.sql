-- +goose Up
-- E15-12 / ADR 0013: a published snapshot references the owner's narration audio by content hash.
-- A shared copy gets its own slide_narrations rows pointing at the same narration_assets row, so
-- ref counting, retention, dedupe and authorization stay the E14 ones. shared_draft_id marks a row
-- that came from a snapshot (never generated for the learner, never charged).
ALTER TABLE slide_narrations ADD COLUMN shared_draft_id uuid REFERENCES publication_drafts(id) ON DELETE SET NULL;
CREATE INDEX slide_narrations_shared_draft ON slide_narrations(shared_draft_id) WHERE shared_draft_id IS NOT NULL;

-- Keeps an asset alive while its snapshot can still be enrolled, even with no copy and no owner row.
-- Rows hold hashes only, no learner id and no text. unpinned_at starts the E14 retention window once
-- the snapshot is no longer enrollable; the sharing retention job sets and clears it.
CREATE TABLE published_narration_pins (
 draft_id uuid NOT NULL REFERENCES publication_drafts(id) ON DELETE CASCADE,
 content_hash text NOT NULL REFERENCES narration_assets(content_hash) ON DELETE CASCADE,
 unpinned_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (draft_id, content_hash)
);
CREATE INDEX published_narration_pins_hash ON published_narration_pins(content_hash);

-- +goose Down
DROP TABLE published_narration_pins;
DROP INDEX slide_narrations_shared_draft;
ALTER TABLE slide_narrations DROP COLUMN shared_draft_id;
