-- +goose Up
-- Existing learners have already found their way around. Only profiles created
-- after this migration receive first-run prompts.
ALTER TABLE learner_profiles
  ADD COLUMN welcome_state text NOT NULL DEFAULT 'completed' CHECK (welcome_state IN ('pending','skipped','completed')),
  ADD COLUMN tour_state text NOT NULL DEFAULT 'completed' CHECK (tour_state IN ('pending','skipped','completed'));
ALTER TABLE learner_profiles ALTER COLUMN welcome_state SET DEFAULT 'pending', ALTER COLUMN tour_state SET DEFAULT 'pending';

-- +goose Down
ALTER TABLE learner_profiles DROP COLUMN welcome_state, DROP COLUMN tour_state;
