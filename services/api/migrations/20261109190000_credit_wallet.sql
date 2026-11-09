-- +goose Up
-- Turn bootstrap ledger into an auditable bucketed wallet.
-- Add daily_grant, subscription_grant and expire kinds; add pricing_version and provenance fields;
-- prohibit new reward writes while historical reward entries stay readable but unspendable.

ALTER TABLE credit_entries DROP CONSTRAINT credit_entries_kind_check;
ALTER TABLE credit_entries ADD CONSTRAINT credit_entries_kind_check
    CHECK (kind IN ('daily_grant', 'purchase', 'subscription_grant', 'spend', 'reserve', 'release', 'refund', 'expire', 'reward'));

ALTER TABLE credit_entries ADD COLUMN pricing_version text;
ALTER TABLE credit_entries ADD COLUMN provenance jsonb NOT NULL DEFAULT '{}'::jsonb;

-- Prohibit new reward writes; historical reward rows remain readable in history.
-- +goose StatementBegin
CREATE FUNCTION prevent_credit_reward_entry() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.kind = 'reward' THEN
        RAISE EXCEPTION 'credit reward writes are prohibited';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER credit_entries_no_reward
    BEFORE INSERT ON credit_entries
    FOR EACH ROW EXECUTE FUNCTION prevent_credit_reward_entry();

-- +goose Down
-- Ledger and provenance history require retention; use a forward migration.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'credit ledger history requires retention; use a forward migration'; END $$;
-- +goose StatementEnd
