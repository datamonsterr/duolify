-- +goose Up
CREATE TABLE pricing_versions (
    version text PRIMARY KEY,
    effective_from timestamptz NOT NULL UNIQUE,
    approved boolean NOT NULL DEFAULT false,
    config jsonb NOT NULL CHECK (jsonb_typeof(config) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (config->>'version' = version),
    CHECK ((config->>'effective_from')::timestamptz = effective_from)
);
INSERT INTO pricing_versions(version, effective_from, approved, config)
VALUES ('usd-launch-2026-11-08', '2026-11-08T00:00:00Z', true, $pricing${
  "version": "usd-launch-2026-11-08",
  "effective_from": "2026-11-08T00:00:00Z",
  "currency": "USD",
  "actions": {
    "preview": { "credits": 25 },
    "outline": { "credits": 55 },
    "lesson_content": { "credits": 140 },
    "narration": { "credits": 50 },
    "tutor_short": { "credits": 5 },
    "tutor_long": { "credits": 8 },
    "quick_note": { "credits": 2 },
    "cached_replay": { "credits": 0 }
  },
  "narration": { "credits": 50, "characters": 1440, "rounding": "ceil_once_per_request" },
  "daily_free": { "credits": 46, "eligible_actions": ["quick_note", "tutor_short", "tutor_long"], "expires": "learner_local_day_end", "spend_first": true },
  "packs": [
    { "sku": "pack_try", "name": "Try", "currency": "USD", "amount_minor": 500, "credits": 400 },
    { "sku": "pack_build", "name": "Build", "currency": "USD", "amount_minor": 1500, "credits": 1350 },
    { "sku": "pack_deep", "name": "Deep", "currency": "USD", "amount_minor": 4000, "credits": 4000 }
  ],
  "plans": [
    { "id": "learn", "name": "Learn", "monthly": { "sku": "learn_monthly", "currency": "USD", "amount_minor": 999 }, "yearly": { "sku": "learn_yearly", "currency": "USD", "amount_minor": 9600 }, "monthly_credits": 600, "concurrent_generation": 1, "benefits": ["Offline course library downloads", "No upsell banners", "Learning history export"] },
    { "id": "plus", "name": "Plus", "monthly": { "sku": "plus_monthly", "currency": "USD", "amount_minor": 1999 }, "yearly": { "sku": "plus_yearly", "currency": "USD", "amount_minor": 19200 }, "monthly_credits": 1600, "concurrent_generation": 1, "benefits": ["Offline course library downloads", "No upsell banners", "Learning history export", "Priority generation queue", "Advanced learning analytics", "Curated voices at the same synthesis rate"] },
    { "id": "pro", "name": "Pro", "monthly": { "sku": "pro_monthly", "currency": "USD", "amount_minor": 3999 }, "yearly": { "sku": "pro_yearly", "currency": "USD", "amount_minor": 38400 }, "monthly_credits": 3600, "concurrent_generation": 2, "benefits": ["Offline course library downloads", "No upsell banners", "Learning history export", "Priority generation queue", "Advanced learning analytics", "Curated voices at the same synthesis rate", "Custom learning analytics exports", "Higher concurrent generation limit"] }
  ],
  "paid_credits": { "expire": false, "rollover_cap": null, "grant_interval": "service_month", "annual_grant": "monthly_anniversary_month_end_clamp" },
  "limits": {
    "protected_cost_ceiling_usd_per_credit": 0.0015,
    "free_learner_usd_per_local_day": 0.08,
    "paid_learner_usd_per_local_day": 5,
    "generation_usd_per_request": 0.5,
    "platform_usd_per_day": 50,
    "preview": { "input_tokens": 2000, "output_tokens": 2500, "slides": 2 },
    "outline": { "input_tokens": 4000, "output_tokens": 6000 },
    "lesson_content": { "input_tokens": 8000, "output_tokens": 14000, "slides": 8, "test_questions": 5, "retry_variants_per_question": 2, "narration_characters": 1440 },
    "tutor": { "cached_context_tokens": 12000, "prompt_tokens": 48000, "short_input_tokens": 500, "short_output_tokens": 250, "long_input_tokens": 1000, "long_output_tokens": 650, "concurrent_free_streams": 1 },
    "quick_note": { "input_tokens": 1200, "output_tokens": 300 },
    "generation_deadline_seconds": 180,
    "tutor_note_deadline_seconds": 30,
    "tts_deadline_seconds": 60,
    "max_repairs_or_fallbacks": 1,
    "timezone_change_delay_hours": 24,
    "adverse_notice_hours": 72,
    "minimum_paid_ai_margin_percent": 70,
    "budget_alert_percent": [80, 100],
    "cost_review_increase_percent": 20,
    "cost_review_days": 7,
    "cache_hit_alert_below_percent": 60
  },
  "development_mappings": {
    "pack_try": "synthetic:pack_try",
    "pack_build": "synthetic:pack_build",
    "pack_deep": "synthetic:pack_deep",
    "learn_monthly": "synthetic:learn_monthly",
    "learn_yearly": "synthetic:learn_yearly",
    "plus_monthly": "synthetic:plus_monthly",
    "plus_yearly": "synthetic:plus_yearly",
    "pro_monthly": "synthetic:pro_monthly",
    "pro_yearly": "synthetic:pro_yearly"
  }
}
$pricing$::jsonb);

-- Development-only synthetic mappings; E12-07/E12-08 own sale activation.
-- Catalogue history is financial data: retain for at least seven years.
-- +goose StatementBegin
CREATE FUNCTION prevent_pricing_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'pricing_versions is immutable; create a new version';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER pricing_versions_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON pricing_versions
    FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();

-- +goose Down
-- Financial catalogue history must survive application rollback.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'pricing history requires seven-year retention; use a forward migration'; END $$;
-- +goose StatementEnd
