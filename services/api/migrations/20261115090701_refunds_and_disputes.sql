-- +goose Up
-- Ticket E7-10: Apply source-attributed refunds and disputes.
-- Retains refund allocations, dispute keys and debt history for seven years in financial backups.

CREATE TABLE credit_reversals (
    reversal_key text PRIMARY KEY,
    order_id uuid NOT NULL REFERENCES credit_checkout_orders(id),
    environment text NOT NULL CHECK(environment='test'),
    event_id text NOT NULL REFERENCES credit_payment_inbox(event_id),
    kind text NOT NULL CHECK(kind IN ('refund', 'chargeback', 'goodwill')),
    amount_minor integer NOT NULL CHECK(amount_minor > 0),
    currency text NOT NULL,
    credits_reversed bigint NOT NULL CHECK(credits_reversed >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE credit_refund_allocations (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reversal_key text NOT NULL REFERENCES credit_reversals(reversal_key),
    source_id bigint NOT NULL REFERENCES credit_entries(id),
    amount_minor integer NOT NULL CHECK(amount_minor >= 0),
    credits_deducted bigint NOT NULL CHECK(credits_deducted >= 0),
    credits_debt bigint NOT NULL CHECK(credits_debt >= 0),
    ledger_id bigint REFERENCES credit_entries(id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE credit_dispute_keys (
    dispute_key text PRIMARY KEY,
    reversal_key text REFERENCES credit_reversals(reversal_key),
    event_id text NOT NULL REFERENCES credit_payment_inbox(event_id),
    order_id uuid NOT NULL REFERENCES credit_checkout_orders(id),
    environment text NOT NULL CHECK(environment='test'),
    status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE credit_debt_history (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    learner_id uuid REFERENCES learners(id) ON DELETE SET NULL,
    owner_reference text NOT NULL,
    reversal_key text NOT NULL REFERENCES credit_reversals(reversal_key),
    source_id bigint NOT NULL REFERENCES credit_entries(id),
    bucket text NOT NULL CHECK(bucket IN ('purchase', 'subscription')),
    amount bigint NOT NULL CHECK(amount > 0),
    status text NOT NULL CHECK(status IN ('active', 'resolved', 'waived_goodwill')),
    resolved_at timestamptz,
    resolution_notes text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX credit_debt_history_learner ON credit_debt_history(learner_id) WHERE status = 'active';
CREATE INDEX credit_debt_history_owner ON credit_debt_history(owner_reference) WHERE status = 'active';

CREATE TRIGGER credit_reversals_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_reversals FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TRIGGER credit_refund_allocations_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_refund_allocations FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TRIGGER credit_dispute_keys_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_dispute_keys FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();

-- +goose StatementBegin
CREATE FUNCTION prevent_credit_debt_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' OR TG_OP = 'TRUNCATE' THEN
        RAISE EXCEPTION 'credit debt history cannot be deleted; use status resolution';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER credit_debt_history_no_delete BEFORE DELETE OR TRUNCATE ON credit_debt_history FOR EACH STATEMENT EXECUTE FUNCTION prevent_credit_debt_deletion();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION prepare_credit_account_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ref text;
BEGIN
    ref := encode(sha256(convert_to(OLD.id::text,'UTF8')),'hex');
    PERFORM release_deleted_credit_jobs(ref);
    UPDATE credit_entries SET learner_id=NULL,owner_reference=ref WHERE learner_id=OLD.id;
    UPDATE credit_timezones SET learner_id=NULL,owner_reference=ref WHERE learner_id=OLD.id;
    UPDATE credit_debt_history SET learner_id=NULL,owner_reference=ref WHERE learner_id=OLD.id;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'refund and debt history requires financial retention; use a forward migration'; END $$;
-- +goose StatementEnd
