-- +goose Up
-- Retry control is separate from immutable financial inbox/provenance. No raw
-- payloads or errors are stored. Retain cumulative counters for seven years.
CREATE TABLE credit_payment_attempts (
 event_id text PRIMARY KEY REFERENCES credit_payment_inbox(event_id),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 total_attempts bigint NOT NULL DEFAULT 0 CHECK(total_attempts>=attempts),
 replay_count integer NOT NULL DEFAULT 0 CHECK(replay_count>=0),
 waiting_for_binding boolean NOT NULL DEFAULT false,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 dead_letter_at timestamptz,
 last_reason_code text NOT NULL DEFAULT '' CHECK(last_reason_code IN ('','provider_unavailable','invalid_evidence','dead_letter','mismatch')),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX credit_payment_attempts_due ON credit_payment_attempts(next_attempt_at) WHERE dead_letter_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION prevent_payment_attempt_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'payment retry provenance requires seven-year retention';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER credit_payment_attempts_retained BEFORE DELETE OR TRUNCATE ON credit_payment_attempts FOR EACH STATEMENT EXECUTE FUNCTION prevent_payment_attempt_deletion();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'financial reconciliation history requires retention; use a forward migration'; END $$;
-- +goose StatementEnd
