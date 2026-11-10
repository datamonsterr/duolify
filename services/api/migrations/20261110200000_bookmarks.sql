-- +goose Up
CREATE TABLE bookmarks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    action_key text NOT NULL,
    course_id uuid REFERENCES courses(id) ON DELETE SET NULL,
    lesson_id uuid,
    slide_id integer CHECK(slide_id IS NULL OR slide_id >= 0),
    target_type text NOT NULL CHECK(target_type IN ('slide','lesson')),
    course_title text NOT NULL DEFAULT '',
    lesson_title text NOT NULL DEFAULT '',
    slide_title text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(learner_id, action_key)
);

CREATE UNIQUE INDEX bookmarks_learner_target ON bookmarks(learner_id, lesson_id, slide_id) NULLS NOT DISTINCT;
CREATE INDEX bookmarks_learner_created ON bookmarks(learner_id, created_at DESC);
CREATE INDEX bookmarks_course ON bookmarks(course_id);
CREATE INDEX bookmarks_lesson ON bookmarks(lesson_id);

-- +goose Down
DROP TABLE bookmarks;
