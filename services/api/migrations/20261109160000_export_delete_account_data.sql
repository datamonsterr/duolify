-- +goose Up
ALTER TABLE credit_entries ADD COLUMN owner_reference text;
DROP TRIGGER credit_entries_immutable ON credit_entries;
UPDATE credit_entries SET owner_reference=encode(sha256(learner_id::text::bytea),'hex');
ALTER TABLE credit_entries ALTER COLUMN learner_id DROP NOT NULL;
ALTER TABLE credit_entries DROP CONSTRAINT credit_entries_learner_id_fkey;
ALTER TABLE credit_entries ADD CONSTRAINT credit_entries_learner_id_fkey
 FOREIGN KEY (learner_id) REFERENCES learners(id) ON DELETE SET NULL;
DROP FUNCTION prevent_credit_entry_mutation();
-- The only permitted mutation is severing the learner link while attaching a
-- pseudonymous SHA-256 reference. Financial terms and ledger order stay fixed.
-- +goose StatementBegin
CREATE FUNCTION protect_credit_entry() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.learner_id IS NOT NULL AND NEW.learner_id IS NULL
    AND NEW.owner_reference ~ '^[0-9a-f]{64}$'
    AND (to_jsonb(NEW)-'learner_id'-'owner_reference')=(to_jsonb(OLD)-'learner_id'-'owner_reference') THEN
   RETURN NEW;
 END IF;
 RAISE EXCEPTION 'credit_entries is append-only except learner anonymization';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER credit_entries_immutable BEFORE UPDATE OR DELETE ON credit_entries
 FOR EACH ROW EXECUTE FUNCTION protect_credit_entry();
CREATE TRIGGER credit_entries_no_truncate BEFORE TRUNCATE ON credit_entries
 FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();

CREATE TABLE account_deletion_tombstones (
 learner_reference text PRIMARY KEY CHECK (learner_reference ~ '^[0-9a-f]{64}$'),
 requested_at timestamptz NOT NULL DEFAULT now(),
 applied_at timestamptz NOT NULL DEFAULT now()
);

-- Durable, retryable PostHog request. Rows are removed after PostHog accepts
-- deletion; the UUID is required to delete the distinct_id and is short-lived.
CREATE TABLE posthog_person_deletion_requests (
 distinct_id uuid PRIMARY KEY,
 requested_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
-- Intentionally irreversible: financial identities and old backups cannot be reconstructed.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'account deletion history is irreversible; use a forward migration'; END $$;
-- +goose StatementEnd
