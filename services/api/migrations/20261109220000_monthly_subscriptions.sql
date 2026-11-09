-- +goose Up
CREATE TABLE credit_subscriptions (
 subscription_id text PRIMARY KEY,
 environment text NOT NULL CHECK(environment='test'),
 order_id uuid NOT NULL UNIQUE REFERENCES credit_checkout_orders(id),
 customer_id text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE credit_subscription_terms (
 period_key text PRIMARY KEY,
 subscription_id text NOT NULL REFERENCES credit_subscriptions(subscription_id),
 invoice_id text NOT NULL UNIQUE,
 pricing_version text NOT NULL REFERENCES pricing_versions(version),
 plan_id text NOT NULL,
 plan_name text NOT NULL,
 entitlement_version text NOT NULL,
 service_start timestamptz NOT NULL,
 paid_through timestamptz NOT NULL CHECK(paid_through>service_start),
 amount_minor integer NOT NULL CHECK(amount_minor>0),
 currency text NOT NULL,
 monthly_credits integer NOT NULL CHECK(monthly_credits>0),
 benefits jsonb NOT NULL CHECK(jsonb_typeof(benefits)='array'),
 concurrent_generation integer NOT NULL CHECK(concurrent_generation>0),
 ledger_id bigint NOT NULL UNIQUE REFERENCES credit_entries(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(subscription_id,service_start,paid_through)
);
CREATE TRIGGER credit_subscriptions_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_subscriptions FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TRIGGER credit_subscription_terms_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_subscription_terms FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
ALTER TABLE credit_payment_results DROP CONSTRAINT credit_payment_results_state_check;
ALTER TABLE credit_payment_results ADD CHECK(state IN ('pending','paid','funded','failed','expired','rejected','duplicate','deleted'));
ALTER TABLE credit_payment_results DROP CONSTRAINT credit_payment_results_check;
ALTER TABLE credit_payment_results ADD CHECK((state IN ('paid','funded') AND payment_key IS NOT NULL AND ledger_id IS NOT NULL) OR (state NOT IN ('paid','funded') AND payment_key IS NULL AND ledger_id IS NULL));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'subscription history requires financial retention; use a forward migration'; END $$;
-- +goose StatementEnd
