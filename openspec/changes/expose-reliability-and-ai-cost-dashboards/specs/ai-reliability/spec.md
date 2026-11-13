## MODIFIED Requirements

### Requirement: Reliability analytics events
The system SHALL emit `$ai_generation` for every call (with `fallback_used`) and `eval_run_completed` for every eval run, as defined in the `analytics` spec. Derived dashboards SHALL publish task quality, fallback rate, error rate, latency, token usage, cache performance, provider cost, and paid margin dimensions without prompts or learner text. An alert MUST fire when the fallback rate or error rate exceeds configurable limits. Notifications MUST be deduplicated while preserving independent server operational counters. Operational enforcement and server counters MUST continue when analytics is unavailable or a learner opts out.

#### Scenario: High fallback rate
- **WHEN** the fallback rate exceeds the limit over the window
- **THEN** a deduplicated alert is raised and independent server counters record the incident

#### Scenario: Reliability dashboard query
- **WHEN** reliability dashboard metrics are retrieved
- **THEN** task quality, fallback, error, and latency dimensions are returned with zero prompts or learner content

### Requirement: Eval data backup and retention
The Golden set dataset is versioned on Hugging Face and its revisions MUST be kept. Eval reports MUST be stored as CI artifacts and in Postgres for 90 days. Operational aggregates and alert configuration MUST be stored in Postgres and included in database dumps.

#### Scenario: Report retention
- **WHEN** an evaluation run completes
- **THEN** its structured report is retained for 90 days

## ADDED Requirements

### Requirement: Eval HTTP access control
Every read under `/v1/eval/` SHALL require a live session via `Accounts.Protect`, a `pricing_admin_grants` grant, and authentication within the last 15 minutes, using the shared pricing admin policy. Report ingest (`POST /v1/eval/reports`) SHALL accept only a service bearer token configured through `EVAL_REPORT_SERVICE_TOKEN`, compared in constant time, and SHALL reject all ingest when that setting is empty. An admin session SHALL NOT authorize ingest. Request bodies SHALL be bounded by `http.MaxBytesReader` to 1 MiB and oversized reports SHALL return HTTP 413 before persistence. Internal errors SHALL be logged server-side and every HTTP 500 SHALL return only a generic error.

This access-control fix SHALL preserve existing Postgres backup and 90-day report retention, operational counters, and PostHog event schemas. It SHALL introduce no new analytics events, prompts, model calls, or model routing changes; existing model eval and fallback requirements continue to apply.

#### Scenario: Anonymous or non-admin dashboard access
- **WHEN** an anonymous or ordinary authenticated learner requests any dashboard, alert list, or report list
- **THEN** the API returns HTTP 401 for anonymous access or HTTP 403 for a non-admin, without exposing operational data

#### Scenario: Admin dashboard access
- **WHEN** an admin with a live session and authentication within the last 15 minutes requests an eval dashboard, alert list, or report list
- **THEN** the API returns HTTP 200 with the requested operational data

#### Scenario: Stale admin authentication
- **WHEN** an admin's last authentication is older than 15 minutes
- **THEN** eval reads return HTTP 403 until the admin reauthenticates

#### Scenario: Forged report ingest
- **WHEN** a report is submitted without the configured service bearer token, with an admin session instead, or while the service token is unset
- **THEN** the API returns HTTP 401 and persists no report

#### Scenario: Authorized service report ingest
- **WHEN** a trusted service submits valid report JSON with the configured service bearer token
- **THEN** the API returns HTTP 201 and persists the report with existing retention

#### Scenario: Oversized report ingest
- **WHEN** an authorized service submits a body exceeding 1 MiB, including a streamed body or trailing whitespace
- **THEN** the API returns HTTP 413 and persists no report

#### Scenario: Internal error redaction
- **WHEN** an authorized eval request encounters an internal failure
- **THEN** the API logs the underlying error and returns HTTP 500 with `internal server error`, without raw database errors or connection details
