# Expose reliability and AI cost dashboards (E10-05)

## Why
Operating AI generation at scale requires continuous visibility into model reliability and cost efficiency. Operators need derived dashboards tracking task quality, fallback rate, error rate, latency, token usage, cache performance, provider cost, and paid margin. In addition, automated alarms must detect cost overruns, performance regressions, and margin erosion, while maintaining strict learner privacy, deduplicating alerts, and continuing server-side operational enforcement even if analytics is down or learners opt out.

## What Changes
- Expose reliability and AI cost dashboards with dimensions for task quality, fallback, error, latency, token usage, cache hits/writes/reads, provider cost, and paid margin without prompts or learner text.
- Implement configurable, deduplicated alerts for:
  - Fallback rate exceeding threshold
  - Error rate exceeding threshold
  - Platform daily budget reaching 80% and 100% of the configured limit (initially USD 50 platform daily budget)
  - Seven-day rolling cost per active learner increasing by +20%
  - Cache hit rate falling below 60%
  - Paid AI gross margin falling below 70%
- Add `ai_cost_budget_alert` (with `scope`, `actual_usd`, and `budget_usd`) alongside `$ai_generation` and `eval_run_completed` to the API analytics allowlist and catalogue.
- Preserve independent server operational counters and ensure operational enforcement continues when analytics is unavailable or a learner opts out.
- Persist operational aggregates, alert configurations, and evaluation reports in Postgres dumps with reports retained for 90 days.
- Require authenticated admin access for every eval dashboard, alert list, and report list, using the pricing admin grant and recent-authentication policy. Restrict report ingest to a separate service bearer token, disable it when unconfigured, cap bodies at 1 MiB, and log internal errors while returning generic HTTP 500 responses (issue #160, defect 1).

## Impact
Touches API eval and telemetry services, database migrations and sqlc queries, analytics allowlist, and data inventory documentation. No new AI model behaviors or changes to existing evaluation gates.
