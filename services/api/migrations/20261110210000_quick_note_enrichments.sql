-- +goose Up
CREATE TABLE quick_note_enrichments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    note_id uuid NOT NULL REFERENCES quick_notes(id) ON DELETE CASCADE,
    learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    summary text NOT NULL,
    tags text[] NOT NULL DEFAULT '{}',
    links text[] NOT NULL DEFAULT '{}',
    version integer NOT NULL DEFAULT 1,
    model text NOT NULL DEFAULT '',
    provider text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(note_id)
);
CREATE INDEX quick_note_enrichments_learner ON quick_note_enrichments(learner_id);

ALTER TABLE quick_notes ADD COLUMN IF NOT EXISTS reservation_id uuid REFERENCES credit_jobs(id) ON DELETE SET NULL;
ALTER TABLE quick_notes ADD COLUMN IF NOT EXISTS quote_id uuid REFERENCES pricing_quotes(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE quick_notes DROP COLUMN IF EXISTS quote_id;
ALTER TABLE quick_notes DROP COLUMN IF EXISTS reservation_id;
DROP TABLE IF EXISTS quick_note_enrichments;
