-- +goose Up
ALTER TABLE learner_profiles
 ADD COLUMN goal text NOT NULL DEFAULT '',
 ADD COLUMN topic text NOT NULL DEFAULT '',
 ADD COLUMN level text NOT NULL DEFAULT '' CHECK (level IN ('','beginner','intermediate','advanced','expert')),
 ADD COLUMN daily_minutes integer CHECK (daily_minutes BETWEEN 1 AND 1440),
 ADD COLUMN daily_xp integer CHECK (daily_xp BETWEEN 1 AND 10000),
 ADD COLUMN reminder_enabled boolean,
 ADD COLUMN reminder_time text NOT NULL DEFAULT '' CHECK (reminder_time='' OR reminder_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
 ADD COLUMN setup_completed boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE learner_profiles DROP COLUMN goal, DROP COLUMN topic, DROP COLUMN level,
 DROP COLUMN daily_minutes, DROP COLUMN daily_xp, DROP COLUMN reminder_enabled,
 DROP COLUMN reminder_time, DROP COLUMN setup_completed;
