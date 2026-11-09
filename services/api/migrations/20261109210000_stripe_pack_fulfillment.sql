-- +goose Up
-- Bind provider Sessions separately: E7-04 illustrative financial terms stay immutable.
CREATE TABLE credit_payment_sessions (
 order_id uuid PRIMARY KEY REFERENCES credit_checkout_orders(id),
 environment text NOT NULL CHECK(environment='test'),
 session_id text NOT NULL UNIQUE,
 price_id text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE credit_payment_inbox (
 event_id text PRIMARY KEY,
 environment text NOT NULL CHECK(environment='test'),
 event_type text NOT NULL,
 session_id text NOT NULL,
 received_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE credit_payment_payloads (
 event_id text PRIMARY KEY REFERENCES credit_payment_inbox(event_id),
 payload jsonb NOT NULL,
 expires_at timestamptz NOT NULL DEFAULT now()+interval '30 days'
);
CREATE INDEX credit_payment_payload_expiry ON credit_payment_payloads(expires_at);
CREATE TABLE credit_payment_results (
 event_id text PRIMARY KEY REFERENCES credit_payment_inbox(event_id),
 order_id uuid REFERENCES credit_checkout_orders(id),
 state text NOT NULL CHECK(state IN ('pending','paid','failed','expired','rejected','duplicate','deleted')),
 payment_key text UNIQUE,
 ledger_id bigint UNIQUE REFERENCES credit_entries(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((state='paid' AND payment_key IS NOT NULL AND ledger_id IS NOT NULL)
 OR (state<>'paid' AND payment_key IS NULL AND ledger_id IS NULL))
);
CREATE UNIQUE INDEX credit_payment_one_purchase_per_order ON credit_payment_results(order_id) WHERE state='paid';
CREATE TRIGGER credit_payment_sessions_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_payment_sessions FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TRIGGER credit_payment_inbox_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_payment_inbox FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TRIGGER credit_payment_results_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_payment_results FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'payment history requires retention; use a forward migration'; END $$;
-- +goose StatementEnd
