-- +goose Up
-- Effort-earned Streak freezes (E5-03). Freezes are earned by Streak milestones only,
-- are applied automatically at rollover, and never involve Credits (ADR 0007).
ALTER TABLE gamification_config
  ADD COLUMN freeze_milestone_days integer NOT NULL DEFAULT 7 CHECK (freeze_milestone_days>=1),
  ADD COLUMN freeze_cap integer NOT NULL DEFAULT 2 CHECK (freeze_cap>=0);

CREATE TABLE streak_freeze_inventory (
  learner_id uuid PRIMARY KEY REFERENCES learners(id) ON DELETE CASCADE,
  held integer NOT NULL DEFAULT 0 CHECK (held>=0),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- One row per milestone reached in a run. granted=false records a cap overflow so a
-- later drop below the cap never grants it retroactively. kind lets E6 reuse the table.
CREATE TABLE streak_freeze_milestones (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  kind text NOT NULL DEFAULT 'streak' CHECK (kind IN ('streak','friend_streak')),
  milestone_days integer NOT NULL CHECK (milestone_days>0),
  run_started_on date NOT NULL,
  reached_on date NOT NULL,
  granted boolean NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (learner_id,kind,milestone_days,run_started_on)
);

-- One automatic protection per learner/local day regardless of source, so a freeze used
-- for a Friend streak (E6) can never be consumed a second time for the same day.
CREATE TABLE streak_day_protections (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  source text NOT NULL DEFAULT 'streak' CHECK (source IN ('streak','friend_streak')),
  protected_on date NOT NULL,
  remaining integer NOT NULL CHECK (remaining>=0),
  streak_length integer NOT NULL CHECK (streak_length>0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (learner_id,protected_on)
);

-- +goose Down
DROP TABLE streak_day_protections;
DROP TABLE streak_freeze_milestones;
DROP TABLE streak_freeze_inventory;
ALTER TABLE gamification_config DROP COLUMN freeze_cap, DROP COLUMN freeze_milestone_days;
