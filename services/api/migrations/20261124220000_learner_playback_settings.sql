-- +goose Up
ALTER TABLE learner_profiles
 ADD COLUMN fullscreen_on_rotate boolean NOT NULL DEFAULT true,
 ADD COLUMN autoplay boolean NOT NULL DEFAULT true,
 ADD COLUMN auto_send_after_dictation boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE learner_profiles DROP COLUMN fullscreen_on_rotate, DROP COLUMN autoplay, DROP COLUMN auto_send_after_dictation;
