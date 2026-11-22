-- +goose Up
-- Indexed "due" predicates for the background sweeps (openspec change reduce-idle-background-load).
-- Plain CREATE INDEX inside the goose transaction, as every migration here does: the tables
-- are small and a short write lock is cheaper than a CONCURRENTLY migration that goose
-- cannot run inside its transaction.

-- Credits: the outbox poll reads only undelivered rows, in creation order.
CREATE INDEX credit_outbox_pending ON credit_outbox (created_at) WHERE dispatched_at IS NULL;

-- Credits: holds whose learner was erased are found without scanning every job.
CREATE INDEX credit_jobs_orphans ON credit_jobs (owner_reference) WHERE learner_id IS NULL AND state IN ('reserved','running');

-- Credits: daily expiry looks at recent grant dates only.
CREATE INDEX credit_entries_daily_grant_date ON credit_entries ((provenance->>'date'), learner_id) WHERE kind = 'daily_grant';

-- Credits: per-day bucket sums (expiry, reservation) for one learner, and the second arm of
-- the learner-or-owner_reference lookups used when a learner was erased and re-linked.
CREATE INDEX credit_entries_learner_date ON credit_entries (learner_id, (provenance->>'date'));
CREATE INDEX credit_entries_owner_reference ON credit_entries (owner_reference) WHERE owner_reference IS NOT NULL;

-- Leagues: finalization reads only groups not yet finalized.
CREATE INDEX league_groups_unfinalized ON league_groups (week_start, id) WHERE finalized_at IS NULL;

-- Friend streaks: the safety-net sweep reads live pairs by staleness.
CREATE INDEX friend_streaks_live_updated ON friend_streaks (updated_at) WHERE current_days > 0;

-- Streaks: the instant the learner's local clock first reads two days past decided_through,
-- which is when the rollover must judge the day that just ended. Rollover reads rows with
-- next_rollover_at <= now instead of converting every learner through pg_timezone_names.
-- 'infinity' means never due (no profile, unknown zone); '-infinity' is the column default
-- and means due, so a row restored from an older dump is rolled over and recomputed.
ALTER TABLE streak_state ADD COLUMN next_rollover_at timestamptz NOT NULL DEFAULT '-infinity';

-- +goose StatementBegin
CREATE FUNCTION streak_next_rollover(decided date, zone text) RETURNS timestamptz
LANGUAGE plpgsql STABLE AS $$
BEGIN
    IF decided IS NULL OR zone IS NULL OR zone = '' THEN
        RETURN 'infinity';
    END IF;
    RETURN ((decided + 2)::timestamp AT TIME ZONE zone);
EXCEPTION WHEN invalid_parameter_value THEN
    RETURN 'infinity';
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION streak_state_set_rollover() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.next_rollover_at := streak_next_rollover(NEW.decided_through,
        (SELECT p.time_zone FROM learner_profiles p WHERE p.learner_id = NEW.learner_id));
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER streak_state_rollover
    BEFORE INSERT OR UPDATE OF decided_through ON streak_state
    FOR EACH ROW EXECUTE FUNCTION streak_state_set_rollover();

-- +goose StatementBegin
CREATE FUNCTION streak_state_zone_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE streak_state SET next_rollover_at = streak_next_rollover(decided_through, NEW.time_zone)
     WHERE learner_id = NEW.learner_id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER streak_state_zone_changed
    AFTER INSERT OR UPDATE OF time_zone ON learner_profiles
    FOR EACH ROW EXECUTE FUNCTION streak_state_zone_changed();

UPDATE streak_state s SET next_rollover_at = streak_next_rollover(s.decided_through,
    (SELECT p.time_zone FROM learner_profiles p WHERE p.learner_id = s.learner_id));

CREATE INDEX streak_state_next_rollover ON streak_state (next_rollover_at);

-- +goose Down
DROP INDEX streak_state_next_rollover;
DROP TRIGGER streak_state_zone_changed ON learner_profiles;
DROP FUNCTION streak_state_zone_changed();
DROP TRIGGER streak_state_rollover ON streak_state;
DROP FUNCTION streak_state_set_rollover();
DROP FUNCTION streak_next_rollover(date, text);
ALTER TABLE streak_state DROP COLUMN next_rollover_at;
DROP INDEX friend_streaks_live_updated;
DROP INDEX league_groups_unfinalized;
DROP INDEX credit_entries_owner_reference;
DROP INDEX credit_entries_learner_date;
DROP INDEX credit_entries_daily_grant_date;
DROP INDEX credit_jobs_orphans;
DROP INDEX credit_outbox_pending;
