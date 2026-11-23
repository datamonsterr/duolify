-- +goose Up
ALTER TABLE learner_profiles
 ADD COLUMN IF NOT EXISTS mascot jsonb NOT NULL DEFAULT '{"mode":"default","accessories":[],"companion_visible":true}'::jsonb
 CHECK (jsonb_typeof(mascot)='object' AND pg_column_size(mascot) <= 2048);

-- +goose Down
ALTER TABLE learner_profiles DROP COLUMN IF EXISTS mascot;
