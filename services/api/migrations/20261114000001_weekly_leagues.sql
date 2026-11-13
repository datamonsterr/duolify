-- +goose Up
-- Weekly leagues (E5-04). Weekly XP is always summed from xp_events; Redis only caches the ranking.
-- Week finalization, promotion and demotion outcomes are E5-05.
ALTER TABLE gamification_config
  ADD COLUMN league_tiers text[] NOT NULL DEFAULT ARRAY['Dawn','Sun','Sky'] CHECK (cardinality(league_tiers)>=1),
  ADD COLUMN league_group_size integer NOT NULL DEFAULT 30 CHECK (league_group_size BETWEEN 2 AND 999),
  ADD COLUMN league_promote_top integer NOT NULL DEFAULT 5 CHECK (league_promote_top>=0),
  ADD COLUMN league_demote_bottom integer NOT NULL DEFAULT 5 CHECK (league_demote_bottom>=0),
  ADD CONSTRAINT league_zones_fit CHECK (league_promote_top+league_demote_bottom<=league_group_size);

-- Groups hold no personal data. size only grows, so slots are never reused after an account deletion.
CREATE TABLE league_groups (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  week_start date NOT NULL CHECK (extract(isodow FROM week_start)=1),
  tier smallint NOT NULL CHECK (tier>=1),
  size integer NOT NULL DEFAULT 0 CHECK (size>=0),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX league_groups_open ON league_groups(week_start,tier,created_at) WHERE size < 999;

CREATE TABLE league_memberships (
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  week_start date NOT NULL CHECK (extract(isodow FROM week_start)=1),
  group_id uuid NOT NULL REFERENCES league_groups(id) ON DELETE CASCADE,
  tier smallint NOT NULL CHECK (tier>=1),
  slot smallint NOT NULL CHECK (slot BETWEEN 1 AND 999),
  joined_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (learner_id,week_start),
  UNIQUE (group_id,slot)
);

-- +goose Down
DROP TABLE league_memberships;
DROP TABLE league_groups;
ALTER TABLE gamification_config DROP CONSTRAINT league_zones_fit,
  DROP COLUMN league_demote_bottom, DROP COLUMN league_promote_top, DROP COLUMN league_group_size, DROP COLUMN league_tiers;
