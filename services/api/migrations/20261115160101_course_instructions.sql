-- +goose Up
ALTER TABLE lesson_generations ADD COLUMN instruction_snapshot_version integer NOT NULL DEFAULT 0 CHECK(instruction_snapshot_version IN (0,1));
-- Jobs that predate this migration have an immutable empty snapshot.
UPDATE lesson_generations SET instruction_snapshot_version=1;
CREATE TABLE steering_config (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 max_characters integer NOT NULL CHECK(max_characters BETWEEN 1 AND 500),
 max_active integer NOT NULL CHECK(max_active BETWEEN 1 AND 20),
 max_total_characters integer NOT NULL CHECK(max_total_characters BETWEEN 1 AND 4000),
 adds_per_hour integer NOT NULL CHECK(adds_per_hour BETWEEN 1 AND 10)
);
INSERT INTO steering_config VALUES(true,500,20,4000,10);
CREATE TABLE course_instructions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
 text text NOT NULL CHECK(char_length(btrim(text)) BETWEEN 1 AND 500),
 scope text NOT NULL CHECK(scope IN ('next_lesson','rest_of_course')),
 status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','applied','deleted')),
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 added_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 applied_lesson_id uuid REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
 deleted_at timestamptz,
 screening_verdict text NOT NULL CHECK(screening_verdict='allow'),
 screening_version text NOT NULL,
 screening_usage jsonb NOT NULL DEFAULT '[]'::jsonb
);
CREATE INDEX course_instructions_future ON course_instructions(course_id,learner_id,status,added_at,id);
CREATE TABLE lesson_instruction_snapshots (
 generation_id uuid NOT NULL REFERENCES lesson_generations(id) ON DELETE CASCADE,
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 instruction_id uuid NOT NULL,
 text text NOT NULL CHECK(char_length(text) BETWEEN 1 AND 500),
 scope text NOT NULL CHECK(scope IN ('next_lesson','rest_of_course')),
 version integer NOT NULL CHECK(version>0),
 added_at timestamptz NOT NULL,
 screening_version text NOT NULL,
 PRIMARY KEY(generation_id,instruction_id)
);
-- Snapshot deliberately owns its text independently of the live instruction row.
-- +goose Down
DROP TABLE lesson_instruction_snapshots,course_instructions,steering_config;
ALTER TABLE lesson_generations DROP COLUMN instruction_snapshot_version;
