-- +goose Up
ALTER TABLE course_enrollments
  ADD COLUMN level_override text NOT NULL DEFAULT '' CHECK (level_override IN ('','beginner','intermediate','advanced','expert')),
  ADD COLUMN last_active_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE learner_profiles
  ADD COLUMN active_course_id uuid REFERENCES courses(id) ON DELETE SET NULL;

CREATE INDEX course_enrollments_learner_active ON course_enrollments(learner_id, last_active_at DESC);

-- +goose Down
DROP INDEX IF EXISTS course_enrollments_learner_active;

ALTER TABLE learner_profiles
  DROP COLUMN active_course_id;

ALTER TABLE course_enrollments
  DROP COLUMN last_active_at,
  DROP COLUMN level_override;
