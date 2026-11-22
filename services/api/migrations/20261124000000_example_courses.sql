-- +goose Up
-- Authored example Courses (example-courses change). A learner's copy of an example is a normal
-- pinned copy: it has no source course, owner or publication draft, only the example's slug and the
-- authored version it is pinned to. One copy per learner and slug.
ALTER TABLE course_copies DROP CONSTRAINT course_copies_source_check;
ALTER TABLE course_copies ADD CONSTRAINT course_copies_source_check CHECK (source IN ('link','invite','marketplace','example'));
ALTER TABLE course_copies ADD COLUMN example_slug text CHECK (example_slug IS NULL OR example_slug ~ '^[a-z0-9-]{1,60}$');
ALTER TABLE course_copies ADD CONSTRAINT course_copies_example_shape CHECK ((source = 'example') = (example_slug IS NOT NULL));
CREATE UNIQUE INDEX course_copies_learner_example ON course_copies (learner_id, example_slug) WHERE example_slug IS NOT NULL;

-- +goose Down
DROP INDEX course_copies_learner_example;
ALTER TABLE course_copies DROP CONSTRAINT course_copies_example_shape;
ALTER TABLE course_copies DROP COLUMN example_slug;
DELETE FROM course_copies WHERE source = 'example';
ALTER TABLE course_copies DROP CONSTRAINT course_copies_source_check;
ALTER TABLE course_copies ADD CONSTRAINT course_copies_source_check CHECK (source IN ('link','invite','marketplace'));
