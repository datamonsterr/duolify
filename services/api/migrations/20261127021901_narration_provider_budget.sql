-- +goose Up
ALTER TABLE narration_assets
    ADD COLUMN provider text NOT NULL DEFAULT 'elevenlabs' CHECK (provider IN ('elevenlabs', 'openrouter')),
    ADD COLUMN timing_source text NOT NULL DEFAULT 'vendor' CHECK (timing_source IN ('vendor', 'estimated'));

-- Account-level operational usage carries no learner or asset identifier.
CREATE TABLE openrouter_narration_budget_ledger (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at timestamptz NOT NULL,
    usd_nanos bigint NOT NULL,
    adjusts uuid UNIQUE REFERENCES openrouter_narration_budget_ledger(id),
    CHECK ((adjusts IS NULL AND usd_nanos > 0) OR (adjusts IS NOT NULL AND usd_nanos <= 0))
);
CREATE INDEX openrouter_narration_budget_occurred_at ON openrouter_narration_budget_ledger(occurred_at);
CREATE TRIGGER openrouter_narration_budget_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON openrouter_narration_budget_ledger
    FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'provider spend history is immutable; use a forward migration'; END $$;
-- +goose StatementEnd
