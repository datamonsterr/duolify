-- +goose Up
-- Streak projection rebuilt from immutable xp_events. Days are the learner-local
-- dates retained on each XP event; decided_through is the last local day the
-- rollover worker has judged. Streak state never grants Credits (ADR 0007).
CREATE TABLE streak_state (
  learner_id uuid PRIMARY KEY REFERENCES learners(id) ON DELETE CASCADE,
  current_days integer NOT NULL DEFAULT 0 CHECK (current_days>=0),
  longest_days integer NOT NULL DEFAULT 0 CHECK (longest_days>=current_days),
  last_active_date date,
  decided_through date NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE streak_days (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  local_date date NOT NULL,
  first_event_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (learner_id,local_date)
);

CREATE TABLE streak_breaks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  broken_on date NOT NULL,
  previous_length integer NOT NULL CHECK (previous_length>0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (learner_id,broken_on)
);

-- +goose Down
DROP TABLE streak_breaks;
DROP TABLE streak_days;
DROP TABLE streak_state;
