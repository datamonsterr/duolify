-- +goose Up
-- E16-05: a steered published copy records the first lesson that was not yet unlocked when the
-- learner confirmed the fork (forked_at itself exists since E15-05). NULL with forked_at set means
-- every lesson was already unlocked. Lessons before the marker stay; reuse stops at it.
ALTER TABLE course_copies ADD COLUMN forked_at_lesson uuid REFERENCES course_lesson_stubs(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE course_copies DROP COLUMN forked_at_lesson;
