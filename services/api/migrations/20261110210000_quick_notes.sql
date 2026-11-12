-- +goose Up
CREATE TABLE quick_notes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    action_key text NOT NULL,
    course_id uuid REFERENCES courses(id) ON DELETE SET NULL,
    lesson_id uuid REFERENCES course_lesson_stubs(id) ON DELETE SET NULL,
    slide_id integer CHECK(slide_id IS NULL OR slide_id >= 0),
    slide_title text NOT NULL DEFAULT '',
    source text NOT NULL CHECK(source IN ('slide','lesson','chat')),
    raw_text text NOT NULL CHECK(length(trim(raw_text)) > 0 AND length(raw_text) <= 2000),
    status text NOT NULL DEFAULT 'enrichment_pending' CHECK(status IN ('enrichment_pending','enriching','enriched','enrichment_failed','raw_only')),
    reviewed boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(learner_id, action_key)
);
CREATE INDEX quick_notes_learner_created ON quick_notes(learner_id, created_at DESC);
CREATE INDEX quick_notes_lesson ON quick_notes(lesson_id);

-- +goose Down
DROP TABLE quick_notes;
