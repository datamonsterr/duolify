-- +goose Up
-- Immutable extension of Pricing config: E12-01's approved document stays intact.
CREATE TABLE pricing_quote_policies (
 version text PRIMARY KEY REFERENCES pricing_versions(version),
 policy jsonb NOT NULL CHECK(jsonb_typeof(policy)='object')
);
INSERT INTO pricing_quote_policies SELECT version, $policy${
 "ttl_seconds":600,"usd_nanos_per_credit":1500000,
 "audio_model":"eleven_flash_v2_5","voices":["default"],"audio_usd_nanos_per_character":40000,
 "action_models":{"preview":"sonnet","outline":"sonnet","lesson_content":"sonnet","quick_note":"haiku","tutor_short":"haiku","tutor_long":"haiku"},
 "models":{
 "sonnet":{"provider":"anthropic","model":"claude-sonnet-5-5","context_max":100000,"input":2000,"output":10000,"cache_write":2500,"cache_read":100},
 "haiku":{"provider":"anthropic","model":"claude-haiku-5-5","context_max":100000,"input":100,"output":500,"cache_write":125,"cache_read":10}
 }
}$policy$::jsonb FROM pricing_versions;
CREATE TRIGGER pricing_quote_policies_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pricing_quote_policies FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TABLE pricing_quotes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid REFERENCES learners(id) ON DELETE SET NULL,
 owner_reference text NOT NULL,
 pricing_version text NOT NULL REFERENCES pricing_quote_policies(version),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at)
);
CREATE INDEX pricing_quotes_owner ON pricing_quotes(learner_id);
-- Permit only FK pseudonymization; financial terms cannot change.
-- +goose StatementBegin
CREATE FUNCTION protect_pricing_quote() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'UPDATE' AND NEW.learner_id IS NULL AND OLD.learner_id IS NOT NULL AND (to_jsonb(NEW)-'learner_id')=(to_jsonb(OLD)-'learner_id') THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'quote history is immutable';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER pricing_quotes_immutable BEFORE UPDATE OR DELETE ON pricing_quotes FOR EACH ROW EXECUTE FUNCTION protect_pricing_quote();
CREATE TRIGGER pricing_quotes_no_truncate BEFORE TRUNCATE ON pricing_quotes FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TABLE pricing_quote_consents (
 quote_id uuid PRIMARY KEY REFERENCES pricing_quotes(id),
 accepted_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER pricing_quote_consents_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pricing_quote_consents FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'quote history requires seven-year retention; use a forward migration'; END $$;
-- +goose StatementEnd
