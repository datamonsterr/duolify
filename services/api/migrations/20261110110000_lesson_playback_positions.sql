-- +goose Up
CREATE TABLE lesson_playback_positions (
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 lesson_id uuid NOT NULL REFERENCES lesson_manifests(lesson_id) ON DELETE CASCADE,
 version integer NOT NULL CHECK (version > 0),
 slide_id text NOT NULL CHECK (slide_id ~ '^[A-Za-z0-9_-]{1,100}$'),
 time_ms integer NOT NULL CHECK (time_ms BETWEEN 0 AND 120000),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (learner_id, lesson_id)
);
-- +goose Down
DROP TABLE lesson_playback_positions;
