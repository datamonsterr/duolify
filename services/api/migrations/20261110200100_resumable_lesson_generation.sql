-- +goose Up
ALTER TABLE lesson_generations ADD COLUMN result_checkpoint jsonb;
CREATE UNIQUE INDEX lesson_generations_one_active ON lesson_generations(lesson_id) WHERE state IN ('queued', 'generating', 'validated');

-- +goose Down
DROP INDEX lesson_generations_one_active;
ALTER TABLE lesson_generations DROP COLUMN result_checkpoint;
