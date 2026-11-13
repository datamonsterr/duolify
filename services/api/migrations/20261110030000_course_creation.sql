-- +goose Up
CREATE TABLE course_creations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 draft_id uuid NOT NULL UNIQUE,
 revision bigint NOT NULL CHECK(revision>0),
 quote_id uuid NOT NULL UNIQUE REFERENCES pricing_quotes(id),
 request jsonb NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','generating','validated','enrolled','failed')),
 reservation_id uuid REFERENCES credit_jobs(id),
 response jsonb,
 course_id uuid,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE courses (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 creation_id uuid NOT NULL UNIQUE REFERENCES course_creations(id) ON DELETE CASCADE,
 title text NOT NULL CHECK(length(trim(title))>0),
 description text NOT NULL DEFAULT '',
 deleted_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE course_units (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
 authored_id text NOT NULL,
 position integer NOT NULL CHECK(position>=0),
 title text NOT NULL CHECK(length(trim(title))>0),
 UNIQUE(course_id,position), UNIQUE(course_id,authored_id)
);
CREATE TABLE course_lesson_stubs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 unit_id uuid NOT NULL REFERENCES course_units(id) ON DELETE CASCADE,
 authored_id text NOT NULL,
 position integer NOT NULL CHECK(position>=0),
 title text NOT NULL CHECK(length(trim(title))>0),
 goal text NOT NULL CHECK(length(trim(goal))>0),
 status text NOT NULL CHECK(status IN ('locked','ready_to_generate')),
 UNIQUE(unit_id,position), UNIQUE(unit_id,authored_id)
);
CREATE TABLE course_enrollments (
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(learner_id,course_id)
);
CREATE INDEX course_creation_owner ON course_creations(learner_id);
CREATE INDEX course_owner ON courses(learner_id);
CREATE INDEX course_lesson_unit ON course_lesson_stubs(unit_id);
-- +goose Down
DROP TABLE course_enrollments,course_lesson_stubs,course_units,courses,course_creations;
