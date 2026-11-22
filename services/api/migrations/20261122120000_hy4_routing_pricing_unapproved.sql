-- +goose Up
-- UNAPPROVED proposal ONLY (ADR 0015). Routes every AI action to tencent/hy4-flash with
-- z-ai/glm-5.3-flash as fallback, via OpenRouter. Credit prices, limits and every other term are a
-- verbatim copy of the latest approved snapshot (usd-character-create-2026-11-16, which carries
-- usd-launch-2026-11-08, usd-quotes-2026-11-09 and usd-design-chat-owner-approved-2026-11-16), so
-- learner prices do not change; only the quote policy (models and provider rates) differs.
-- Approved versions and their quote policies are immutable and untouched. The row stays approved=false:
-- it is never selected as the active catalogue until the owner approves it.
--
-- Rates are integer USD nanodollars per token, rounded UP from the OpenRouter list prices of
-- 2026-11-22 (USD per 1M tokens, input / output / cache read) so operational cost is never understated:
--   tencent/hy4-flash   0.035 / 0.1405 / 0.0015  ->  35 / 141 / 2   (no cache write price: input rate)
--   z-ai/glm-5.3-flash  0.0399 / 0.0784 / 0.03   ->  40 /  79 / 30  (no cache write price: input rate)
-- The haiku and haiku_fallback keys are aliases of the same two models: platform screening and
-- sharing rates still look the "haiku" key up by name.
INSERT INTO pricing_versions(version, effective_from, approved, config)
VALUES ('usd-hy4-routing-2026-11-22-unapproved', '2026-11-22T12:00:00Z', false, $pricing${
  "version": "usd-hy4-routing-2026-11-22-unapproved",
  "effective_from": "2026-11-22T12:00:00Z",
  "currency": "USD",
  "design_chat_decision": "owner_approved",
  "actions": {
    "preview": {
      "credits": 25
    },
    "outline": {
      "credits": 55
    },
    "lesson_content": {
      "credits": 140
    },
    "narration": {
      "credits": 50
    },
    "tutor_short": {
      "credits": 5
    },
    "tutor_long": {
      "credits": 8
    },
    "quick_note": {
      "credits": 2
    },
    "cached_replay": {
      "credits": 0
    },
    "setup_chat": {
      "credits": 0
    },
    "course_brief_chat": {
      "credits": 0
    },
    "outline_chat_change": {
      "credits": 5
    },
    "outline_replan": {
      "credits": 55
    },
    "character_create": {
      "credits": 25
    }
  },
  "narration": {
    "credits": 50,
    "characters": 1440,
    "rounding": "ceil_once_per_request"
  },
  "daily_free": {
    "credits": 46,
    "eligible_actions": [
      "quick_note",
      "tutor_short",
      "tutor_long",
      "outline_chat_change"
    ],
    "expires": "learner_local_day_end",
    "spend_first": true
  },
  "packs": [
    {
      "sku": "pack_try",
      "name": "Try",
      "currency": "USD",
      "amount_minor": 500,
      "credits": 400
    },
    {
      "sku": "pack_build",
      "name": "Build",
      "currency": "USD",
      "amount_minor": 1500,
      "credits": 1350
    },
    {
      "sku": "pack_deep",
      "name": "Deep",
      "currency": "USD",
      "amount_minor": 4000,
      "credits": 4000
    }
  ],
  "plans": [
    {
      "id": "learn",
      "name": "Learn",
      "monthly": {
        "sku": "learn_monthly",
        "currency": "USD",
        "amount_minor": 999
      },
      "yearly": {
        "sku": "learn_yearly",
        "currency": "USD",
        "amount_minor": 9600
      },
      "monthly_credits": 600,
      "concurrent_generation": 1,
      "benefits": [
        "Offline course library downloads",
        "No upsell banners",
        "Learning history export"
      ]
    },
    {
      "id": "plus",
      "name": "Plus",
      "monthly": {
        "sku": "plus_monthly",
        "currency": "USD",
        "amount_minor": 1999
      },
      "yearly": {
        "sku": "plus_yearly",
        "currency": "USD",
        "amount_minor": 19200
      },
      "monthly_credits": 1600,
      "concurrent_generation": 1,
      "benefits": [
        "Offline course library downloads",
        "No upsell banners",
        "Learning history export",
        "Priority generation queue",
        "Advanced learning analytics",
        "Curated voices at the same synthesis rate"
      ]
    },
    {
      "id": "pro",
      "name": "Pro",
      "monthly": {
        "sku": "pro_monthly",
        "currency": "USD",
        "amount_minor": 3999
      },
      "yearly": {
        "sku": "pro_yearly",
        "currency": "USD",
        "amount_minor": 38400
      },
      "monthly_credits": 3600,
      "concurrent_generation": 2,
      "benefits": [
        "Offline course library downloads",
        "No upsell banners",
        "Learning history export",
        "Priority generation queue",
        "Advanced learning analytics",
        "Curated voices at the same synthesis rate",
        "Custom learning analytics exports",
        "Higher concurrent generation limit"
      ]
    }
  ],
  "paid_credits": {
    "expire": false,
    "rollover_cap": null,
    "grant_interval": "service_month",
    "annual_grant": "monthly_anniversary_month_end_clamp"
  },
  "limits": {
    "protected_cost_ceiling_usd_per_credit": 0.0015,
    "free_learner_usd_per_local_day": 0.08,
    "paid_learner_usd_per_local_day": 5,
    "generation_usd_per_request": 0.5,
    "platform_usd_per_day": 50,
    "free_concurrent_generation": 1,
    "preview": {
      "input_tokens": 2000,
      "output_tokens": 2500,
      "slides": 2
    },
    "outline": {
      "input_tokens": 4000,
      "output_tokens": 6000
    },
    "lesson_content": {
      "input_tokens": 8000,
      "output_tokens": 14000,
      "slides": 8,
      "test_questions": 5,
      "retry_variants_per_question": 2,
      "narration_characters": 1440
    },
    "tutor": {
      "cached_context_tokens": 12000,
      "prompt_tokens": 48000,
      "short_input_tokens": 500,
      "short_output_tokens": 250,
      "long_input_tokens": 1000,
      "long_output_tokens": 650,
      "concurrent_free_streams": 1
    },
    "quick_note": {
      "input_tokens": 1200,
      "output_tokens": 300
    },
    "setup_chat": {
      "input_tokens": 2000,
      "output_tokens": 1000
    },
    "course_brief_chat": {
      "input_tokens": 2000,
      "output_tokens": 1000,
      "attempt_timeout_ms": 3000,
      "max_attempts": 3,
      "max_cost_nanos": 2100000,
      "input_nanos_per_token": 100,
      "output_nanos_per_token": 500,
      "requests_per_hour": 30,
      "platform_requests_per_day": 10000
    },
    "outline_chat_change": {
      "input_tokens": 12000,
      "output_tokens": 2000,
      "timeout_ms": 10000,
      "max_attempts": 2,
      "max_cost_nanos": 5000000
    },
    "generation_deadline_seconds": 180,
    "tutor_note_deadline_seconds": 30,
    "tts_deadline_seconds": 60,
    "max_repairs_or_fallbacks": 1,
    "timezone_change_delay_hours": 24,
    "adverse_notice_hours": 72,
    "minimum_paid_ai_margin_percent": 70,
    "budget_alert_percent": [
      80,
      100
    ],
    "cost_review_increase_percent": 20,
    "cost_review_days": 7,
    "cache_hit_alert_below_percent": 60,
    "outline_replan": {
      "input_tokens": 4000,
      "output_tokens": 6000,
      "timeout_ms": 90000,
      "max_attempts": 2,
      "max_cost_nanos": 500000000
    },
    "character_create": {
      "input_tokens": 4000,
      "output_tokens": 2000,
      "timeout_ms": 90000,
      "max_attempts": 2,
      "max_cost_nanos": 50000000,
      "count": 10,
      "day_attempts": 5,
      "minute_attempts": 2,
      "prompt_chars": 1500,
      "screen_input_tokens": 18000,
      "screen_output_tokens": 200,
      "screen_timeout_ms": 10000,
      "screen_max_attempts": 2
    }
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
  },
  "design_chat_policy": {
    "setup_chat": "platform_paid",
    "course_brief_chat": "platform_paid",
    "outline_chat_change": "valid_delivered_proposal_even_if_discarded",
    "undelivered_proposal": "release_reservation",
    "manual_edits_apply_discard_undo": "free"
  },
  "character_create_decision": "owner_approved"
}
$pricing$::jsonb);
INSERT INTO pricing_quote_policies(version, policy)
SELECT 'usd-hy4-routing-2026-11-22-unapproved', policy || $policy${"action_models": {"preview": "hy4_flash", "outline": "hy4_flash", "lesson_content": "hy4_flash", "tutor_short": "hy4_flash", "tutor_long": "hy4_flash", "quick_note": "hy4_flash", "setup_chat": "hy4_flash", "course_brief_chat": "hy4_flash", "outline_chat_change": "hy4_flash", "outline_replan": "hy4_flash", "character_create": "hy4_flash"}, "fallback_models": {"hy4_flash": "glm_flash", "haiku": "haiku_fallback"}, "models": {"hy4_flash": {"provider": "openrouter", "model": "tencent/hy4-flash", "context_max": 1048576, "input": 35, "output": 141, "cache_write": 35, "cache_read": 2}, "glm_flash": {"provider": "openrouter", "model": "z-ai/glm-5.3-flash", "context_max": 1048576, "input": 40, "output": 79, "cache_write": 40, "cache_read": 30}, "haiku": {"provider": "openrouter", "model": "tencent/hy4-flash", "context_max": 1048576, "input": 35, "output": 141, "cache_write": 35, "cache_read": 2}, "haiku_fallback": {"provider": "openrouter", "model": "z-ai/glm-5.3-flash", "context_max": 1048576, "input": 40, "output": 79, "cache_write": 40, "cache_read": 30}}}$policy$::jsonb
FROM pricing_quote_policies WHERE version = 'usd-quotes-2026-11-09';

-- +goose Down
-- Pricing history is immutable financial data; use a forward migration.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'pricing history requires seven-year retention; use a forward migration'; END $$;
-- +goose StatementEnd
