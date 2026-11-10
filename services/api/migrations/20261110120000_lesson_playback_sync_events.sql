-- +goose Up
CREATE TABLE lesson_playback_sync_events (
  action_key text NOT NULL PRIMARY KEY CHECK (action_key ~ '^[A-Za-z0-9_-]{1,100}$'),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  lesson_id uuid NOT NULL REFERENCES lesson_manifests(lesson_id) ON DELETE CASCADE,
  version integer NOT NULL CHECK (version > 0),
  slide_id text NOT NULL CHECK (slide_id ~ '^[A-Za-z0-9_-]{1,100}$'),
  time_ms integer NOT NULL CHECK (time_ms BETWEEN 0 AND 120000),
  occurred_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_lesson_playback_sync_events_learner_lesson ON lesson_playback_sync_events(learner_id, lesson_id);

ALTER TABLE lesson_playback_positions ADD COLUMN IF NOT EXISTS occurred_at timestamptz NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE lesson_playback_positions DROP COLUMN IF EXISTS occurred_at;
DROP TABLE lesson_playback_sync_events;
