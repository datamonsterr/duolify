-- +goose Up
ALTER TABLE learner_profiles
 ADD COLUMN appearance text NOT NULL DEFAULT 'system' CHECK (appearance IN ('system','light','dark')),
 ADD COLUMN motion text NOT NULL DEFAULT 'system' CHECK (motion IN ('system','reduce')),
 ADD COLUMN narration boolean NOT NULL DEFAULT true,
 ADD COLUMN narration_speed numeric(3,2) NOT NULL DEFAULT 1.00 CHECK (narration_speed BETWEEN 0.5 AND 2.0),
 ADD COLUMN captions boolean NOT NULL DEFAULT true,
 ADD COLUMN text_size numeric(3,2) NOT NULL DEFAULT 1.00 CHECK (text_size BETWEEN 0.8 AND 1.5),
 ADD COLUMN ui_sounds boolean NOT NULL DEFAULT false,
 ADD COLUMN haptics text NOT NULL DEFAULT 'system' CHECK (haptics IN ('system','on','off'));

-- +goose Down
ALTER TABLE learner_profiles DROP COLUMN appearance, DROP COLUMN motion,
 DROP COLUMN narration, DROP COLUMN narration_speed, DROP COLUMN captions,
 DROP COLUMN text_size, DROP COLUMN ui_sounds, DROP COLUMN haptics;
