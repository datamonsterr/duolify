-- +goose Up
CREATE TABLE credit_subscription_lifecycle (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 subscription_id text NOT NULL REFERENCES credit_subscriptions(subscription_id),
 event_id text REFERENCES credit_payment_inbox(event_id),
 provider_status text NOT NULL,
 period_start timestamptz NOT NULL,
 period_end timestamptz NOT NULL CHECK(period_end>period_start),
 cancel_at_period_end boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX credit_subscription_lifecycle_latest ON credit_subscription_lifecycle(subscription_id,id DESC);
CREATE TABLE credit_subscription_transitions (
 request_id uuid PRIMARY KEY,
 subscription_id text NOT NULL REFERENCES credit_subscriptions(subscription_id),
 kind text NOT NULL CHECK(kind IN ('plan','cancel','resume')),
 effective_at timestamptz NOT NULL,
 pricing_version text NOT NULL REFERENCES pricing_versions(version),
 terms jsonb NOT NULL,
 price_id text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX credit_subscription_one_plan_change ON credit_subscription_transitions(subscription_id,effective_at) WHERE kind='plan';
CREATE TRIGGER credit_subscription_lifecycle_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_subscription_lifecycle FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TRIGGER credit_subscription_transitions_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_subscription_transitions FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'subscription lifecycle requires financial retention; use a forward migration'; END $$;
-- +goose StatementEnd
