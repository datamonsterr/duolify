-- +goose Up
-- Runtime activation markers for seeded approved versions.
-- Unifies quote resolution with the active runtime catalogue.
INSERT INTO pricing_activation_events (version, activated_at)
SELECT v.version, v.effective_from
FROM pricing_versions v
WHERE v.version IN ('usd-launch-2026-11-08', 'usd-quotes-2026-11-09')
ON CONFLICT (version) DO NOTHING;

-- +goose Down
-- Runtime activation markers are financial audit history; retain them.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'pricing runtime history requires retention; use a forward migration'; END $$;
-- +goose StatementEnd
