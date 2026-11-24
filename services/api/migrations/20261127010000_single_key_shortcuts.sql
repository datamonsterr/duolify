-- +goose Up
ALTER TABLE learner_profiles ADD COLUMN single_key_shortcuts boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE learner_profiles DROP COLUMN single_key_shortcuts;
