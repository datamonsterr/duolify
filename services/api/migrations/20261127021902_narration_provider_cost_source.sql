-- +goose Up
-- An unapproved cost-policy source for the pricing admin approval workflow.
INSERT INTO pricing_versions(version,effective_from,approved,config)
SELECT 'usd-openrouter-narration-cost-unapproved','2099-01-01T00:00:00Z',false,
       config || '{"version":"usd-openrouter-narration-cost-unapproved","effective_from":"2099-01-01T00:00:00Z"}'::jsonb
FROM pricing_versions WHERE version='usd-voice-preview-2026-11-23' AND approved;

INSERT INTO pricing_quote_policies(version,policy)
SELECT 'usd-openrouter-narration-cost-unapproved', policy || '{
  "openrouter_audio_rates": {
    "hexgrad/kokoro-82m": {"usd_nanos_per_character":990,"max_usd_nanos_per_character":4000},
    "microsoft/mai-voice-2.1-flash": {"usd_nanos_per_character":15000,"max_usd_nanos_per_character":15000}
  }
}'::jsonb
FROM pricing_quote_policies WHERE version='usd-voice-preview-2026-11-23';

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'pricing history requires seven-year retention; use a forward migration'; END $$;
-- +goose StatementEnd
