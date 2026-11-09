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
