-- +goose Up
-- Voice quick note formatting: the title and cleaned body sit beside the summary of the same
-- enrichment; the raw text in quick_notes is never touched.
ALTER TABLE quick_note_enrichments ADD COLUMN title text NOT NULL DEFAULT '';
ALTER TABLE quick_note_enrichments ADD COLUMN formatted_body text NOT NULL DEFAULT '';

-- A voice note is formatted only when it holds a grant: a free turn taken at save or an explicit
-- paid Format request. The grant is spent (or refunded) when the job ends, so one note never takes
-- two free turns and an edit never formats again by itself.
ALTER TABLE quick_notes ADD COLUMN format_grant text CHECK (format_grant IN ('free','paid'));
ALTER TABLE quick_notes ADD COLUMN format_grant_day date;

-- Free formatting turns per Learner local day (the same clock as the voice daily cap).
CREATE TABLE quick_note_format_turns (
    learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    day date NOT NULL,
    used integer NOT NULL CHECK (used >= 0),
    PRIMARY KEY (learner_id, day)
);

-- +goose Down
DROP TABLE IF EXISTS quick_note_format_turns;
ALTER TABLE quick_notes DROP COLUMN IF EXISTS format_grant_day;
ALTER TABLE quick_notes DROP COLUMN IF EXISTS format_grant;
ALTER TABLE quick_note_enrichments DROP COLUMN IF EXISTS formatted_body;
ALTER TABLE quick_note_enrichments DROP COLUMN IF EXISTS title;
