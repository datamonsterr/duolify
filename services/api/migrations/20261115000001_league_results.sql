-- +goose Up
-- Final weekly league results (E5-05). Weekly XP stays summed from xp_events; a result freezes the
-- week's final standing so promotion and demotion happen once. Results never carry Credits or text.
ALTER TABLE league_groups ADD COLUMN finalized_at timestamptz;

CREATE TABLE league_week_results (
  -- Deterministic from learner and week, so a rebuilt result keeps its analytics event_id.
  id uuid PRIMARY KEY,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  week_start date NOT NULL CHECK (extract(isodow FROM week_start)=1),
  group_id uuid NOT NULL REFERENCES league_groups(id) ON DELETE CASCADE,
  tier_from smallint NOT NULL CHECK (tier_from>=1),
  tier_to smallint NOT NULL CHECK (tier_to>=1),
  final_rank integer NOT NULL CHECK (final_rank>=1),
  members integer NOT NULL CHECK (members>=1),
  weekly_xp bigint NOT NULL CHECK (weekly_xp>=0),
  outcome text NOT NULL CHECK (outcome IN ('promoted','demoted','stayed')),
  finalized_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (learner_id,week_start)
);
CREATE INDEX league_week_results_group ON league_week_results(group_id);

-- +goose Down
DROP TABLE league_week_results;
ALTER TABLE league_groups DROP COLUMN finalized_at;
