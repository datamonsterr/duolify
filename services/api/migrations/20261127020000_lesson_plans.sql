-- +goose Up
ALTER TABLE course_lesson_stubs
    ADD COLUMN plan jsonb CHECK (jsonb_typeof(plan) = 'object');

-- +goose StatementBegin
CREATE FUNCTION reject_lesson_plan_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Lesson plans are immutable' USING ERRCODE = '23514';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER course_lesson_plan_immutable
    BEFORE UPDATE ON course_lesson_stubs
    FOR EACH ROW WHEN (OLD.plan IS DISTINCT FROM NEW.plan)
    EXECUTE FUNCTION reject_lesson_plan_update();

-- +goose Down
DROP TRIGGER course_lesson_plan_immutable ON course_lesson_stubs;
DROP FUNCTION reject_lesson_plan_update();
ALTER TABLE course_lesson_stubs DROP COLUMN plan;
