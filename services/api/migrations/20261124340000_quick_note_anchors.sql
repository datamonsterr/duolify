-- +goose Up
-- A Quick note taken in the Lesson player may point at a moment: the Slide is the note's slide_id,
-- this row adds the playback time, the optional region (normalised 0..1 on the 16:9 stage) and
-- whether the text was typed or dictated. Erased with the note.
CREATE TABLE quick_note_anchors (
    note_id uuid PRIMARY KEY REFERENCES quick_notes(id) ON DELETE CASCADE,
    learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    playback_ms integer NOT NULL CHECK (playback_ms >= 0 AND playback_ms <= 86400000),
    rect_x double precision,
    rect_y double precision,
    rect_w double precision,
    rect_h double precision,
    input_kind text NOT NULL DEFAULT 'typed' CHECK (input_kind IN ('typed','voice')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (rect_x IS NULL AND rect_y IS NULL AND rect_w IS NULL AND rect_h IS NULL)
        OR (rect_x >= 0 AND rect_y >= 0 AND rect_w > 0 AND rect_h > 0 AND rect_x + rect_w <= 1.000001 AND rect_y + rect_h <= 1.000001)
    )
);
CREATE INDEX quick_note_anchors_learner ON quick_note_anchors(learner_id);

-- +goose Down
DROP TABLE quick_note_anchors;
