-- +goose Up
-- Pinned learner-owned copies of published courses (E15-03). A copy is a normal course row, so
-- progress, tests, notes, chat and XP stay learner-specific; the pin records the published version.
ALTER TABLE courses ALTER COLUMN creation_id DROP NOT NULL;
ALTER TABLE course_lesson_stubs ADD COLUMN reused_from_snapshot boolean NOT NULL DEFAULT false;

CREATE TABLE course_copies (
  copy_course_id uuid PRIMARY KEY REFERENCES courses(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  -- Provenance only: the copy keeps working when the source course, draft or owner is deleted.
  source_course_id uuid REFERENCES courses(id) ON DELETE SET NULL,
  draft_id uuid REFERENCES publication_drafts(id) ON DELETE SET NULL,
  version integer NOT NULL CHECK (version > 0),
  content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  source text NOT NULL DEFAULT 'link' CHECK (source IN ('link','invite','marketplace')),
  created_at timestamptz NOT NULL DEFAULT now()
);
-- One copy per learner and source course.
CREATE UNIQUE INDEX course_copies_learner_source ON course_copies (learner_id, source_course_id) WHERE source_course_id IS NOT NULL;
CREATE INDEX course_copies_source ON course_copies (source_course_id) WHERE source_course_id IS NOT NULL;
CREATE INDEX course_copies_created ON course_copies (learner_id, created_at);

-- +goose Down
DROP TABLE course_copies;
ALTER TABLE course_lesson_stubs DROP COLUMN reused_from_snapshot;
ALTER TABLE courses ALTER COLUMN creation_id SET NOT NULL;
