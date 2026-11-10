-- +goose Up
CREATE TABLE gamification_config (
  id boolean PRIMARY KEY DEFAULT true CHECK (id),
  version text NOT NULL,
  base_xp integer NOT NULL CHECK (base_xp BETWEEN 1 AND 10000),
  perfect_bonus_xp integer NOT NULL CHECK (perfect_bonus_xp BETWEEN 0 AND 10000),
  default_goal integer NOT NULL CHECK (default_goal > 0),
  goal_options integer[] NOT NULL CHECK (cardinality(goal_options)>0 AND 0<ALL(goal_options) AND default_goal=ANY(goal_options))
);
INSERT INTO gamification_config(version,base_xp,perfect_bonus_xp,default_goal,goal_options)
VALUES ('launch-v1',10,5,20,ARRAY[10,20,30,50]);

-- Historical summaries do not prove first-try perfection; never infer it from replay.
ALTER TABLE lesson_completion_summaries ADD COLUMN first_try_perfect boolean NOT NULL DEFAULT false,
ADD COLUMN duration_ms bigint NOT NULL DEFAULT 0 CHECK (duration_ms>=0);

CREATE TABLE xp_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  lesson_id uuid NOT NULL,
  course_id uuid NOT NULL,
  completion_id uuid NOT NULL UNIQUE,
  amount integer NOT NULL CHECK (amount>0),
  perfect boolean NOT NULL,
  config_version text NOT NULL,
  earned_at timestamptz NOT NULL,
  local_date date NOT NULL,
  time_zone text NOT NULL,
  UNIQUE (learner_id,lesson_id)
);
CREATE INDEX xp_events_local_day ON xp_events(learner_id,local_date);
CREATE INDEX xp_events_week ON xp_events(earned_at,learner_id);

CREATE TABLE daily_goal_achievements (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  local_date date NOT NULL,
  goal_xp integer NOT NULL CHECK (goal_xp>0),
  achieved_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (learner_id,local_date)
);

-- +goose StatementBegin
CREATE FUNCTION protect_gamification_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='TRUNCATE' THEN
    RAISE EXCEPTION 'gamification history cannot be truncated';
  END IF;
  IF TG_OP='UPDATE' OR EXISTS(SELECT 1 FROM learners WHERE id=OLD.learner_id) THEN
    RAISE EXCEPTION 'gamification history is immutable for the account lifetime';
  END IF;
  RETURN OLD;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER xp_events_immutable BEFORE UPDATE OR DELETE ON xp_events FOR EACH ROW EXECUTE FUNCTION protect_gamification_history();
CREATE TRIGGER daily_goal_achievements_immutable BEFORE UPDATE OR DELETE ON daily_goal_achievements FOR EACH ROW EXECUTE FUNCTION protect_gamification_history();

CREATE TRIGGER xp_events_no_truncate BEFORE TRUNCATE ON xp_events FOR EACH STATEMENT EXECUTE FUNCTION protect_gamification_history();
CREATE TRIGGER daily_goal_achievements_no_truncate BEFORE TRUNCATE ON daily_goal_achievements FOR EACH STATEMENT EXECUTE FUNCTION protect_gamification_history();

-- +goose Down
DROP TABLE daily_goal_achievements;
DROP TABLE xp_events;
DROP FUNCTION protect_gamification_history();
ALTER TABLE lesson_completion_summaries DROP COLUMN first_try_perfect, DROP COLUMN duration_ms;
DROP TABLE gamification_config;
