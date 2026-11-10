-- +goose Up
CREATE TABLE lesson_test_variant_snapshots (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES lesson_test_sessions(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  lesson_stub_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  topic_id text NOT NULL,
  question_id text NOT NULL,
  variant_of text NOT NULL,
  source text NOT NULL CHECK(source IN ('pool','pool_exhausted_fallback','safe_source_fallback')),
  reason text,
  prompt text NOT NULL CHECK(length(trim(prompt))>0),
  choices jsonb NOT NULL,
  answer_index integer NOT NULL CHECK(answer_index>=0),
  explanation text NOT NULL CHECK(length(trim(explanation))>0),
  instruction_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(session_id, question_id)
);

CREATE INDEX lesson_test_variant_snapshots_session ON lesson_test_variant_snapshots(session_id);
CREATE INDEX lesson_test_variant_snapshots_learner ON lesson_test_variant_snapshots(learner_id);
CREATE INDEX lesson_test_variant_snapshots_stub ON lesson_test_variant_snapshots(lesson_stub_id);

-- +goose Down
DROP TABLE IF EXISTS lesson_test_variant_snapshots;
