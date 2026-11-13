-- +goose Up
CREATE TABLE credit_checkout_orders (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 request_id uuid NOT NULL,
 learner_id uuid REFERENCES learners(id) ON DELETE SET NULL,
 owner_reference text NOT NULL CHECK(owner_reference ~ '^[0-9a-f]{64}$'),
 pricing_version text NOT NULL REFERENCES pricing_versions(version),
 sku text NOT NULL,
 amount_minor bigint NOT NULL CHECK(amount_minor>=0),
 currency text NOT NULL,
 credits bigint NOT NULL CHECK(credits>=0),
 billing_period text NOT NULL CHECK(billing_period IN ('one_time','monthly','yearly')),
 platform text NOT NULL CHECK(platform IN ('web','desktop')),
 return_to text NOT NULL CHECK(return_to IN ('/credits','/learn','/notes','/courses')),
 quote_id uuid REFERENCES pricing_quotes(id),
 illustrative boolean NOT NULL DEFAULT true CHECK(illustrative),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(owner_reference,request_id)
);
-- Orders preserve their original financial terms. Only identity may be severed.
-- +goose StatementBegin
CREATE FUNCTION protect_checkout_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.learner_id IS NOT NULL AND NEW.learner_id IS NULL
    AND (to_jsonb(NEW)-'learner_id')=(to_jsonb(OLD)-'learner_id') THEN
  RETURN NEW;
 END IF;
 RAISE EXCEPTION 'checkout order terms are immutable';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER checkout_orders_immutable BEFORE UPDATE OR DELETE ON credit_checkout_orders
 FOR EACH ROW EXECUTE FUNCTION protect_checkout_order();
CREATE TRIGGER checkout_orders_no_truncate BEFORE TRUNCATE ON credit_checkout_orders
 FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'financial order history is irreversible; use a forward migration'; END $$;
-- +goose StatementEnd
