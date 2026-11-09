## MODIFIED Requirements

### Requirement: Operations events
The API SHALL emit `backup_completed` and `backup_failed` (size_bytes, duration_ms, ok), `restore_drill_completed` (duration_ms, ok), `eval_run_completed` (task, score, threshold, passed), and `ai_cost_budget_alert` (scope, actual_usd, budget_usd) from workers, API, and CI, with `source` set to `api` or `ci`. Operational jobs without a Learner SHALL use the reserved system UUID `00000000-0000-4000-8000-000000000011`, platform `web`, and app_version from build information (`development` for local builds). This UUID never identifies a Learner. `ANALYTICS_OPT_OUT=true` SHALL suppress server analytics globally, including operations events. No database contents, object keys, credentials, provider errors, prompts, or free text SHALL be sent by operations emitters.

#### Scenario: Cost budget alert fired
- **WHEN** spend reaches 80% or 100% of the platform daily budget
- **THEN** `ai_cost_budget_alert` is emitted with `scope`, `actual_usd`, and `budget_usd` under the system operational UUID without learner content

#### Scenario: Analytics opted out during alert
- **WHEN** server analytics is opted out or unavailable
- **THEN** server alert counters and operational enforcement continue without failing requests
