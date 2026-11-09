-- +goose Up
CREATE TABLE eval_alert_configs (
 id text PRIMARY KEY DEFAULT 'default',
 platform_daily_budget_usd numeric NOT NULL DEFAULT 50.00,
 daily_budget_alert_80_percent boolean NOT NULL DEFAULT true,
 daily_budget_alert_100_percent boolean NOT NULL DEFAULT true,
 learner_cost_increase_threshold numeric NOT NULL DEFAULT 0.20,
 cache_hit_rate_threshold numeric NOT NULL DEFAULT 0.60,
 paid_margin_threshold numeric NOT NULL DEFAULT 0.70,
 fallback_rate_threshold numeric NOT NULL DEFAULT 0.15,
 error_rate_threshold numeric NOT NULL DEFAULT 0.05,
 alert_window_minutes integer NOT NULL DEFAULT 60,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO eval_alert_configs(id) VALUES('default');

CREATE TABLE eval_alert_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 scope text NOT NULL,
 alert_key text NOT NULL UNIQUE,
 actual_usd numeric NOT NULL DEFAULT 0,
 budget_usd numeric NOT NULL DEFAULT 0,
 properties jsonb NOT NULL DEFAULT '{}',
 status text NOT NULL DEFAULT 'firing',
 notified_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX eval_alert_events_scope ON eval_alert_events(scope, created_at);

CREATE TABLE eval_reports (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 task text NOT NULL,
 dataset_revision text NOT NULL,
 prompt_revision text NOT NULL DEFAULT '',
 model text NOT NULL,
 score numeric NOT NULL,
 threshold numeric NOT NULL,
 passed boolean NOT NULL,
 report_json jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT (now() + interval '90 days')
);
CREATE INDEX eval_reports_created ON eval_reports(created_at);
CREATE INDEX eval_reports_expires ON eval_reports(expires_at);

CREATE TABLE eval_operational_aggregates (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 day date NOT NULL,
 task text NOT NULL,
 total_calls bigint NOT NULL DEFAULT 0,
 fallback_calls bigint NOT NULL DEFAULT 0,
 error_calls bigint NOT NULL DEFAULT 0,
 total_latency_ms bigint NOT NULL DEFAULT 0,
 input_tokens bigint NOT NULL DEFAULT 0,
 output_tokens bigint NOT NULL DEFAULT 0,
 cache_write_tokens bigint NOT NULL DEFAULT 0,
 cache_read_tokens bigint NOT NULL DEFAULT 0,
 cost_nanos bigint NOT NULL DEFAULT 0,
 credit_revenue_cents bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(day, task)
);
CREATE INDEX eval_aggregates_day ON eval_operational_aggregates(day);

-- +goose Down
DROP TABLE eval_operational_aggregates,eval_reports,eval_alert_events,eval_alert_configs;
