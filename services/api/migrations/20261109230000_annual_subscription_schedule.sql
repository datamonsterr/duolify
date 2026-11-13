-- +goose Up
-- Every service-month business key is durable; grants remain append-only ledger rows.
CREATE TABLE credit_subscription_schedule (
 grant_key text PRIMARY KEY,
 period_key text NOT NULL REFERENCES credit_subscription_terms(period_key),
 month_index integer NOT NULL CHECK(month_index BETWEEN 0 AND 11),
 due_at timestamptz NOT NULL,
 UNIQUE(period_key,month_index)
);
CREATE INDEX credit_subscription_schedule_due ON credit_subscription_schedule(due_at);
CREATE UNIQUE INDEX credit_subscription_grant_reference ON credit_entries(reference) WHERE kind='subscription_grant';
CREATE TABLE credit_subscription_revocations (
 period_key text PRIMARY KEY REFERENCES credit_subscription_terms(period_key),
 event_id text NOT NULL REFERENCES credit_payment_inbox(event_id),
 reason text NOT NULL CHECK(reason IN ('refunded','disputed')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE credit_subscription_cancellations (
 period_key text PRIMARY KEY REFERENCES credit_subscription_terms(period_key),
 event_id text NOT NULL REFERENCES credit_payment_inbox(event_id),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER credit_subscription_schedule_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_subscription_schedule FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TRIGGER credit_subscription_revocations_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_subscription_revocations FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TRIGGER credit_subscription_cancellations_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_subscription_cancellations FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'annual subscription history requires financial retention; use a forward migration'; END $$;
-- +goose StatementEnd
