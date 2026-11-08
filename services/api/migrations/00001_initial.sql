-- +goose Up
CREATE TABLE learners (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE credit_entries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    learner_id uuid NOT NULL REFERENCES learners(id),
    amount bigint NOT NULL CHECK (amount <> 0),
    kind text NOT NULL CHECK (kind IN ('purchase', 'reward', 'spend', 'reserve', 'release', 'refund')),
    reference text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX credit_entries_learner_id_id_idx ON credit_entries (learner_id, id);

-- Bootstrap enforces append-only history; corrections must be new entries.
-- +goose StatementBegin
CREATE FUNCTION prevent_credit_entry_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'credit_entries is append-only';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER credit_entries_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_entries
    FOR EACH STATEMENT EXECUTE FUNCTION prevent_credit_entry_mutation();

-- +goose Down
DROP TABLE credit_entries;
DROP FUNCTION prevent_credit_entry_mutation();
DROP TABLE learners;
