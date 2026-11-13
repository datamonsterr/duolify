-- +goose Up
-- Shared Friend streaks (E6-06). One state row per friendship; pair-day and milestone
-- keys make every settlement idempotent. Deleting the friendship (remove, block) or
-- either learner cascades the whole pair state away.
ALTER TABLE gamification_config
  ADD COLUMN friend_streak_milestone_days integer NOT NULL DEFAULT 7 CHECK (friend_streak_milestone_days>=1);

CREATE TABLE friend_streaks (
  learner_low uuid NOT NULL,
  learner_high uuid NOT NULL,
  current_days integer NOT NULL DEFAULT 0 CHECK (current_days>=0),
  longest_days integer NOT NULL DEFAULT 0 CHECK (longest_days>=0),
  run_no integer NOT NULL DEFAULT 0 CHECK (run_no>=0),
  cursor_low date,
  cursor_high date,
  run_start_low date,
  run_start_high date,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (learner_low, learner_high),
  FOREIGN KEY (learner_low, learner_high) REFERENCES friendships(learner_low, learner_high) ON DELETE CASCADE
);

-- One row per settled shared day: each friend's own local date and the outcome.
CREATE TABLE friend_streak_days (
  id uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
  learner_low uuid NOT NULL,
  learner_high uuid NOT NULL,
  low_date date NOT NULL,
  high_date date NOT NULL,
  run_no integer NOT NULL CHECK (run_no>0),
  outcome text NOT NULL CHECK (outcome IN ('extended','protected','broken')),
  length integer NOT NULL CHECK (length>=0),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (learner_low, learner_high, low_date, high_date),
  FOREIGN KEY (learner_low, learner_high) REFERENCES friend_streaks(learner_low, learner_high) ON DELETE CASCADE
);

CREATE TABLE friend_streak_milestones (
  learner_low uuid NOT NULL,
  learner_high uuid NOT NULL,
  run_no integer NOT NULL CHECK (run_no>0),
  milestone_days integer NOT NULL CHECK (milestone_days>0),
  reached_low date NOT NULL,
  reached_high date NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (learner_low, learner_high, run_no, milestone_days),
  FOREIGN KEY (learner_low, learner_high) REFERENCES friend_streaks(learner_low, learner_high) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE friend_streak_milestones;
DROP TABLE friend_streak_days;
DROP TABLE friend_streaks;
ALTER TABLE gamification_config DROP COLUMN friend_streak_milestone_days;
