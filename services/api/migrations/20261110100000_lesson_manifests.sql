-- +goose Up
CREATE TABLE lesson_manifests (
 lesson_id uuid PRIMARY KEY REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 version integer NOT NULL CHECK(version > 0),
 manifest_key text NOT NULL CHECK(manifest_key LIKE 'lessons/%'),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX lesson_manifest_owner ON lesson_manifests(learner_id);
-- +goose Down
DROP TABLE lesson_manifests;
