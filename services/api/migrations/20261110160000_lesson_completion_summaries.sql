-- +goose Up
CREATE TABLE lesson_completion_summaries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
  lesson_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  summary text NOT NULL CHECK(length(trim(summary))>0),
  weak_topics jsonb NOT NULL DEFAULT '[]'::jsonb,
  completed_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(learner_id, lesson_id)
);

CREATE INDEX lesson_completion_summaries_learner ON lesson_completion_summaries(learner_id);
CREATE INDEX lesson_completion_summaries_course ON lesson_completion_summaries(course_id, completed_at ASC);

-- +goose Down
DROP TABLE IF EXISTS lesson_completion_summaries;
