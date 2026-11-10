-- +goose Up
ALTER TABLE course_lesson_stubs DROP CONSTRAINT IF EXISTS course_lesson_stubs_status_check;
ALTER TABLE course_lesson_stubs ADD CONSTRAINT course_lesson_stubs_status_check CHECK(status IN ('locked','ready_to_generate','generating','current','done'));

CREATE TABLE lesson_generations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
  lesson_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  quote_id uuid NOT NULL REFERENCES pricing_quotes(id),
  reservation_id uuid REFERENCES credit_jobs(id),
  state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','generating','validated','completed','failed')),
  error_code text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE generated_lessons (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  lesson_stub_id uuid NOT NULL UNIQUE REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  title text NOT NULL CHECK(length(trim(title))>0),
  manifest jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE lesson_slides (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  lesson_stub_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  position integer NOT NULL CHECK(position>=0),
  slide_id text NOT NULL,
  html text NOT NULL,
  duration_ms integer NOT NULL CHECK(duration_ms>0),
  narration text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(lesson_stub_id, position)
);

CREATE TABLE lesson_questions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  lesson_stub_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  topic_id text NOT NULL,
  position integer NOT NULL CHECK(position>=0),
  prompt text NOT NULL CHECK(length(trim(prompt))>0),
  choices jsonb NOT NULL,
  answer_index integer NOT NULL CHECK(answer_index>=0),
  explanation text NOT NULL CHECK(length(trim(explanation))>0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(lesson_stub_id, position)
);

CREATE TABLE lesson_test_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  lesson_stub_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  state text NOT NULL DEFAULT 'in_progress' CHECK(state IN ('in_progress','passed','abandoned')),
  question_queue jsonb NOT NULL,
  resolved_topics jsonb NOT NULL DEFAULT '[]'::jsonb,
  first_try_correct integer NOT NULL DEFAULT 0,
  variants_served integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE lesson_test_answers (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES lesson_test_sessions(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  question_id text NOT NULL,
  topic_id text NOT NULL,
  selected_index integer NOT NULL CHECK(selected_index>=0),
  correct boolean NOT NULL,
  is_variant boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE lesson_completion_outbox (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
  lesson_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  passed_at timestamptz NOT NULL DEFAULT now(),
  state text NOT NULL DEFAULT 'passed',
  UNIQUE(learner_id, lesson_id)
);

CREATE INDEX lesson_generations_learner ON lesson_generations(learner_id);
CREATE INDEX lesson_generations_lesson ON lesson_generations(lesson_id);
CREATE INDEX generated_lessons_course ON generated_lessons(course_id);
CREATE INDEX lesson_slides_stub ON lesson_slides(lesson_stub_id);
CREATE INDEX lesson_questions_stub ON lesson_questions(lesson_stub_id);
CREATE INDEX lesson_test_sessions_stub ON lesson_test_sessions(lesson_stub_id);
CREATE INDEX lesson_test_answers_session ON lesson_test_answers(session_id);
CREATE INDEX lesson_completion_outbox_learner ON lesson_completion_outbox(learner_id);

-- +goose Down
DROP TABLE IF EXISTS lesson_completion_outbox;
DROP TABLE IF EXISTS lesson_test_answers;
DROP TABLE IF EXISTS lesson_test_sessions;
DROP TABLE IF EXISTS lesson_questions;
DROP TABLE IF EXISTS lesson_slides;
DROP TABLE IF EXISTS generated_lessons;
DROP TABLE IF EXISTS lesson_generations;

ALTER TABLE course_lesson_stubs DROP CONSTRAINT IF EXISTS course_lesson_stubs_status_check;
ALTER TABLE course_lesson_stubs ADD CONSTRAINT course_lesson_stubs_status_check CHECK(status IN ('locked','ready_to_generate'));
