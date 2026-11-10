-- +goose Up
ALTER TABLE pricing_audit ADD COLUMN override_reason text CHECK (override_reason IS NULL OR length(btrim(override_reason)) BETWEEN 1 AND 1000);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'pricing override audit requires retention; use a forward migration'; END $$;
-- +goose StatementEnd
