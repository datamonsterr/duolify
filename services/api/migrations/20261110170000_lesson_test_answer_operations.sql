-- +goose Up
CREATE TABLE lesson_test_answer_operations (
  operation_key text PRIMARY KEY CHECK (operation_key ~ '^[A-Za-z0-9_-]{1,100}$'),
  session_id uuid NOT NULL REFERENCES lesson_test_sessions(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  lesson_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  question_id text NOT NULL,
  choice_index integer NOT NULL CHECK (choice_index >= 0),
  response_payload jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_lesson_test_answer_operations_session ON lesson_test_answer_operations(session_id);
CREATE INDEX idx_lesson_test_answer_operations_learner_lesson ON lesson_test_answer_operations(learner_id, lesson_id);

-- +goose Down
DROP TABLE IF EXISTS lesson_test_answer_operations;
