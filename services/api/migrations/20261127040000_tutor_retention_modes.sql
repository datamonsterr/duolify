-- +goose Up
ALTER TABLE tutor_turns ADD COLUMN guarded_question_id text;
CREATE INDEX tutor_turns_guarded_question ON tutor_turns(learner_id, guarded_question_id) WHERE guarded_question_id IS NOT NULL;

CREATE TABLE lesson_teach_backs (
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  lesson_stub_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  turn smallint NOT NULL DEFAULT 0 CHECK(turn BETWEEN 0 AND 3),
  messages jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(messages) = 'array'),
  response jsonb NOT NULL DEFAULT '{"turn":0}'::jsonb CHECK(jsonb_typeof(response) = 'object'),
  processing_token uuid,
  processing_until timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (learner_id, lesson_stub_id),
  CHECK ((processing_token IS NULL) = (processing_until IS NULL)),
  CHECK (turn < 3 OR (messages = '[]'::jsonb AND processing_token IS NULL))
);
CREATE INDEX lesson_teach_backs_with_messages ON lesson_teach_backs(updated_at, learner_id, lesson_stub_id) WHERE messages <> '[]'::jsonb;
CREATE INDEX lesson_teach_backs_lesson ON lesson_teach_backs(lesson_stub_id);

-- +goose Down
DROP TABLE lesson_teach_backs;
DROP INDEX tutor_turns_guarded_question;
ALTER TABLE tutor_turns DROP COLUMN guarded_question_id;
