-- +goose Up
CREATE TABLE narration_budget_ledger (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    characters bigint NOT NULL,
    adjusts uuid UNIQUE REFERENCES narration_budget_ledger(id)
);
CREATE INDEX narration_budget_ledger_occurred_at ON narration_budget_ledger(occurred_at);
CREATE TRIGGER narration_budget_ledger_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON narration_budget_ledger FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
-- Preserve known spending before this migration, independently of future asset retention.
INSERT INTO narration_budget_ledger(occurred_at, characters)
SELECT created_at, characters FROM narration_assets WHERE characters > 0
UNION ALL
SELECT completed_at, characters FROM narration_regenerations WHERE status='completed' AND characters > 0 AND completed_at IS NOT NULL;

-- +goose Down
DROP TABLE narration_budget_ledger;
