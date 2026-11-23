## 1. Dashboards, counters and alert rules

- [x] 1.1 Operational aggregates, alert config and report tables (`20261110060000_eval_dashboards_and_ai_cost_alerts.sql`)
- [x] 1.2 Reliability and cost dashboards without prompts or learner text (`eval.GetDashboards`)
- [x] 1.3 Alert rules: fallback rate, error rate, platform budget 80%/100%, 7-day cost per learner, cache hit rate, paid margin (`eval.CheckAndTriggerAlerts`)
- [x] 1.4 `ai_cost_budget_alert` in the analytics allowlist and catalogue
- [x] 1.5 Admin-only eval reads, service-token ingest, 1 MiB cap, generic 500s

## 2. Scheduling and deduplication

- [x] 2.1 River periodic job `reliability_alerts` (periodic ID `reliability-alerts`, every 5 minutes, run on start, unique by period) registered in the worker via `worker.NewWithAlerts` and started from `worker.RunWithAlerts`
- [x] 2.2 Alert history insert is atomic (`ON CONFLICT (alert_key) DO NOTHING`), so concurrent workers fire each alert once per window
- [x] 2.3 Scheduled run also deletes eval reports past their 90-day `expires_at`
- [x] 2.4 Integration test: two worker clients run the periodic job, one alert row and one `ai_cost_budget_alert` event per scope, rechecks deduplicate

## 3. Pricing failure alerts

- [x] 3.1 Pricing config-load failure and scheduled-flip failure raise deduplicated operational alerts (`pricing_config_load_failed`, `pricing_scheduled_flip_failed`) through `RuntimeCatalogue.SetAlertSink`, in addition to logs
