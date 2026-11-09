## ADDED Requirements

### Requirement: Correlated redacted operational traces
The API SHALL propagate its trusted request ID and span context to gRPC engine
calls and River jobs. Retries SHALL preserve correlation and log distinct attempt
spans without tokens, prompts, private text, object keys or raw errors. Readiness
SHALL distinguish postgres, redis, engine and object_storage failures with safe
states. Backup exhaustion, queue outages and dependency incidents SHALL each
raise one alert per observed incident, independent of analytics availability.

#### Scenario: Follow a failed action
- **WHEN** a request calls the engine and enqueues a backup that exhausts retries
- **THEN** HTTP, RPC and worker spans share request and trace IDs and one terminal alert fires

#### Scenario: Repeated unhealthy readiness
- **WHEN** a dependency fails across repeated probes
- **THEN** readiness names the failing dependency and one incident alert fires until recovery

### Requirement: Bounded operational data
Logs and traces SHALL be redacted and bounded by size rotation. Metrics SHALL
have seven-day collector retention. Dashboards SHALL be derived; operational
configuration SHALL live in Git under infra. The data inventory SHALL document
River correlation metadata, recovery checks and derived-data disposal. No AI
behavior changes; existing evaluation and fallback requirements SHALL remain.

#### Scenario: Restore correlation metadata
- **WHEN** an encrypted synthetic backup is restored
- **THEN** safe River metadata survives and no new learner data store is required
