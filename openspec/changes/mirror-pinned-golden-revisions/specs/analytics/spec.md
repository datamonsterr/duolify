## MODIFIED Requirements

### Requirement: Operations events
The API SHALL emit `backup_completed` and `backup_failed` (size_bytes, duration_ms, ok), `restore_drill_completed` (duration_ms, ok) and `eval_run_completed` (task, score, threshold, passed, dataset_revision, dataset_source) from workers and CI, with `source` set to `api` or `ci`. Operational jobs without a Learner SHALL use the reserved system UUID `00000000-0000-4000-8000-000000000011`, platform `web`, and app_version from build information (`development` for local builds). This UUID never identifies a Learner. `ANALYTICS_OPT_OUT=true` SHALL suppress server analytics globally, including operations events. No database contents, object keys, credentials, provider errors or free text SHALL be sent by backup emitters.

#### Scenario: Eval run reported
- **WHEN** an evaluation task completes
- **THEN** `eval_run_completed` is emitted with task, score, threshold, passed status, dataset revision, and dataset source metadata
