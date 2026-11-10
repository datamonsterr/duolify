-- +goose Up
ALTER TABLE lesson_slides ADD COLUMN IF NOT EXISTS topic_ids text[] NOT NULL DEFAULT '{}'::text[];
ALTER TABLE lesson_questions ADD COLUMN IF NOT EXISTS variant_of text;

CREATE TABLE lesson_retry_pools (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  lesson_stub_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  topic_id text NOT NULL,
  variants jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(lesson_stub_id, topic_id)
);

CREATE INDEX lesson_retry_pools_stub ON lesson_retry_pools(lesson_stub_id);
CREATE INDEX lesson_retry_pools_learner ON lesson_retry_pools(learner_id);

-- +goose Down
DROP TABLE IF EXISTS lesson_retry_pools;
ALTER TABLE lesson_questions DROP COLUMN IF EXISTS variant_of;
ALTER TABLE lesson_slides DROP COLUMN IF EXISTS topic_ids;
