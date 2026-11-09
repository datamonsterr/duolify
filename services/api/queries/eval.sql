-- name: GetAlertConfig :one
SELECT * FROM eval_alert_configs
WHERE id = 'default' LIMIT 1;

-- name: UpsertAlertConfig :one
INSERT INTO eval_alert_configs (
  id,
  platform_daily_budget_usd,
  daily_budget_alert_80_percent,
  daily_budget_alert_100_percent,
  learner_cost_increase_threshold,
  cache_hit_rate_threshold,
  paid_margin_threshold,
  fallback_rate_threshold,
  error_rate_threshold,
  alert_window_minutes,
  updated_at
) VALUES (
  'default', $1, $2, $3, $4, $5, $6, $7, $8, $9, now()
)
ON CONFLICT (id) DO UPDATE SET
  platform_daily_budget_usd = EXCLUDED.platform_daily_budget_usd,
  daily_budget_alert_80_percent = EXCLUDED.daily_budget_alert_80_percent,
  daily_budget_alert_100_percent = EXCLUDED.daily_budget_alert_100_percent,
  learner_cost_increase_threshold = EXCLUDED.learner_cost_increase_threshold,
  cache_hit_rate_threshold = EXCLUDED.cache_hit_rate_threshold,
  paid_margin_threshold = EXCLUDED.paid_margin_threshold,
  fallback_rate_threshold = EXCLUDED.fallback_rate_threshold,
  error_rate_threshold = EXCLUDED.error_rate_threshold,
  alert_window_minutes = EXCLUDED.alert_window_minutes,
  updated_at = now()
RETURNING *;

-- name: InsertAlertEvent :one
INSERT INTO eval_alert_events (
  scope,
  alert_key,
  actual_usd,
  budget_usd,
  properties,
  status
) VALUES (
  $1, $2, $3, $4, $5, $6
)
ON CONFLICT (alert_key) DO NOTHING
RETURNING *;

-- name: GetAlertEventByKey :one
SELECT * FROM eval_alert_events
WHERE alert_key = $1 LIMIT 1;

-- name: ListRecentAlertEvents :many
SELECT * FROM eval_alert_events
ORDER BY created_at DESC
LIMIT $1;

-- name: InsertEvalReport :one
INSERT INTO eval_reports (
  task,
  dataset_revision,
  prompt_revision,
  model,
  score,
  threshold,
  passed,
  report_json
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: ListEvalReports :many
SELECT * FROM eval_reports
ORDER BY created_at DESC
LIMIT $1;

-- name: RecordAggregateCall :one
INSERT INTO eval_operational_aggregates (
  day,
  task,
  total_calls,
  fallback_calls,
  error_calls,
  total_latency_ms,
  input_tokens,
  output_tokens,
  cache_write_tokens,
  cache_read_tokens,
  cost_nanos,
  credit_revenue_cents
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
ON CONFLICT (day, task) DO UPDATE SET
  total_calls = eval_operational_aggregates.total_calls + EXCLUDED.total_calls,
  fallback_calls = eval_operational_aggregates.fallback_calls + EXCLUDED.fallback_calls,
  error_calls = eval_operational_aggregates.error_calls + EXCLUDED.error_calls,
  total_latency_ms = eval_operational_aggregates.total_latency_ms + EXCLUDED.total_latency_ms,
  input_tokens = eval_operational_aggregates.input_tokens + EXCLUDED.input_tokens,
  output_tokens = eval_operational_aggregates.output_tokens + EXCLUDED.output_tokens,
  cache_write_tokens = eval_operational_aggregates.cache_write_tokens + EXCLUDED.cache_write_tokens,
  cache_read_tokens = eval_operational_aggregates.cache_read_tokens + EXCLUDED.cache_read_tokens,
  cost_nanos = eval_operational_aggregates.cost_nanos + EXCLUDED.cost_nanos,
  credit_revenue_cents = eval_operational_aggregates.credit_revenue_cents + EXCLUDED.credit_revenue_cents
RETURNING *;

-- name: GetDailyAggregates :many
SELECT * FROM eval_operational_aggregates
WHERE day >= $1
ORDER BY day DESC, task ASC;

-- name: GetOverallAggregateTotals :one
SELECT
  COALESCE(SUM(total_calls), 0)::bigint AS total_calls,
  COALESCE(SUM(fallback_calls), 0)::bigint AS fallback_calls,
  COALESCE(SUM(error_calls), 0)::bigint AS error_calls,
  COALESCE(SUM(total_latency_ms), 0)::bigint AS total_latency_ms,
  COALESCE(SUM(input_tokens), 0)::bigint AS input_tokens,
  COALESCE(SUM(output_tokens), 0)::bigint AS output_tokens,
  COALESCE(SUM(cache_write_tokens), 0)::bigint AS cache_write_tokens,
  COALESCE(SUM(cache_read_tokens), 0)::bigint AS cache_read_tokens,
  COALESCE(SUM(cost_nanos), 0)::bigint AS cost_nanos,
  COALESCE(SUM(credit_revenue_cents), 0)::bigint AS credit_revenue_cents
FROM eval_operational_aggregates
WHERE day >= $1;
