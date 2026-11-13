-- +goose Up
-- Grant and expire eligible daily credits (E7-02).
-- Enforce Postgres grant uniqueness by learner and local date;
-- track timezone changes with 24-hour delayed effective dates in seven-year ledger backups.

CREATE UNIQUE INDEX credit_entries_daily_grant_uniq
    ON credit_entries (learner_id, (provenance->>'date'))
    WHERE kind = 'daily_grant';

CREATE TABLE credit_timezones (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    learner_id uuid REFERENCES learners(id) ON DELETE SET NULL,
    owner_reference text,
    time_zone text NOT NULL,
    effective_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX credit_timezones_learner_id_idx ON credit_timezones (learner_id);

-- +goose StatementBegin
CREATE FUNCTION protect_credit_timezone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND OLD.learner_id IS NOT NULL AND NEW.learner_id IS NULL
       AND NEW.owner_reference ~ '^[0-9a-f]{64}$'
       AND OLD.id = NEW.id AND OLD.time_zone = NEW.time_zone
       AND OLD.effective_at = NEW.effective_at AND OLD.created_at = NEW.created_at THEN
       RETURN NEW;
    END IF;
    RAISE EXCEPTION 'credit_timezones is append-only except learner anonymization';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER credit_timezones_immutable
    BEFORE UPDATE OR DELETE ON credit_timezones
    FOR EACH ROW EXECUTE FUNCTION protect_credit_timezone();

CREATE TRIGGER credit_timezones_no_truncate
    BEFORE TRUNCATE ON credit_timezones
    FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();

-- +goose Down
-- Ledger and timezone history require retention; use a forward migration.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'credit ledger and timezone history require retention; use a forward migration'; END $$;
-- +goose StatementEnd
