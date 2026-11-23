# Design

Structured JSON spans use request_id, trace_id, span_id and parent_span_id. HTTP
creates the trusted root; gRPC metadata and River metadata carry only validated
correlation fields. Scheduled jobs create a root on insertion. River metadata
does not change backup date uniqueness. Span records contain only fixed operation
names, elapsed time and outcomes, never payloads, tokens or raw provider errors.
Rust instruments futures, including stream polling, without recording arguments.

Operational counters have fixed label sets and are independent of PostHog.
Backup terminal failures alert once per durable job, queue failures once per
outage, and dependency failures once per unhealthy interval. Recovery rearms the
incident. Queue monitoring observes terminal worker attempts and database failure.
Counters and deduplication are disposable process state; durable job identity
and terminal attempt checks prevent per-attempt alerts. Continuing outages remain
deduplicated until recovery; terminal jobs are deduplicated for 24 hours within
a 4096-entry cap that evicts the oldest terminal record. No exactly-once guarantee
across process restarts is claimed. Metrics are served on private loopback ports.

Readiness probes postgres, redis, engine and authenticated application S3 access
concurrently with a shared deadline. Responses contain dependency names and safe
states only. Missing configuration is unhealthy. PostHog uses a bounded channel,
nonblocking enqueue and bounded shutdown; events are schema filtered before SDK
delivery. Existing emitters own persisted learner-consent checks. Global opt-out
suppresses all delivery. Operational metrics never contain learner identifiers.

Local Docker JSON logs rotate at 10 MiB with three files per container. A
Git-backed operational scrape/alert config describes the private endpoints,
seven-day metric retention and fixed alert rules. No Railway changes or deploys.
Traces/logs and metrics are derived and excluded from database recovery. River
metadata is covered by existing encrypted dumps and focused roundtrip checks.

Dependency references checked before implementation: Go gRPC interceptors and
metadata (pkg.go.dev/google.golang.org/grpc), River middleware and job controls
(pkg.go.dev/github.com/riverqueue/river), tonic Request and tracing Instrument
(docs.rs/tonic and docs.rs/tracing), PostHog Config (pkg.go.dev/github.com/posthog/posthog-go),
AWS S3 HeadBucket (docs.aws.amazon.com/AmazonS3/latest/API/API_HeadBucket.html),
Docker logging (docs.docker.com/engine/logging/drivers/json-file/).
