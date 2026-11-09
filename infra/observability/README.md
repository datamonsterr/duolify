# Operational telemetry

HTTP creates a trusted request ID and trace root. Go gRPC sends `x-request-id`
and W3C `traceparent`; Rust `engine.rpc` spans validate these fields and create
their own span ID. River middleware preserves correlation in job metadata,
independent of date uniqueness. Each retry has a distinct span. Scheduled and
legacy jobs without metadata get new roots. JSON spans are the collector boundary;
filter by `request_id` or `trace_id` across API, engine and worker stdout.

No request/response bodies, auth headers, URLs, queries, object keys, raw errors,
prompts or completions enter these spans. Provider failures use a fixed code.
River receives sanitized attempt errors and no raw panic traces. The application
S3 readiness probe authenticates `HeadBucket` with application credentials only.
`/readyz` returns separate postgres, redis, engine and object_storage states.

API and worker expose `/metrics` on separate private listeners. Local processes
default to loopback ports 9091 and 9092; Compose binds inside its private network,
without publishing either port. `OPERATIONS_ADDR` overrides the binding.
Never route it through public ingress. The optional `observability` Compose
profile supplies a pinned Prometheus image and Git-backed scrape/alert rules;
only its UI is published, to loopback 9090. No Railway configuration is changed.

Metrics count HTTP, gRPC, enqueue, dependency and worker calls, errors and total
duration with a fixed label set. Mean duration is
`rate(duolify_duration_seconds_sum[5m]) / rate(duolify_duration_seconds_count[5m])`;
error rate is `rate(duolify_operations_total{ok="false"}[5m])`.
`duolify_alerts_total{kind="backup|queue|dependency"}` counts distinct incidents.
Backup alerts occur after retry exhaustion, never on a recoverable attempt.
Queue inserts and a 15-second database watchdog detect queue outages, including
idle periods. Dependency probes rearm after recovery. Alert state is process
memory, bounded to 4096 entries; terminal job dedupe expires after 24 hours and
old terminal entries can be evicted at capacity. Continuing dependency and queue
outages remain deduplicated until recovery. Deduplication does not promise
exactly-once delivery across restarts.
The structured `operational alert` record is the immediate local signal;
Prometheus evaluates the supplied rules without PostHog. Notification routing
requires separately provisioned operator infrastructure and credentials.

PostHog uses two bounded queues and nonblocking enqueue/drop behavior. Shutdown
drains the adapter then uses the SDK's three-second shutdown limit. Invalid UUIDs,
uncatalogued events and private properties are discarded. Common properties are
required. Global opt-out disables delivery; existing learner emitters query
persisted consent. Metrics and alerts remain active in both cases.

API, worker and engine JSON logs rotate at 10 MiB, retaining three files per
container (at most 30 MiB each). Prometheus retains metrics for at most seven
days or 256 MB. Both stores are derived and can be deleted/recreated. Git holds
operational config and dashboard queries. No new product table is created;
existing encrypted dumps cover River metadata. The focused fixture proves its
roundtrip without storing learner text. Production log collectors must enforce
equivalent bounds before accepting traffic; no production collector is applied
by this ticket.

References: [Docker rotation](https://docs.docker.com/engine/logging/drivers/json-file/),
[Prometheus configuration](https://prometheus.io/docs/prometheus/latest/configuration/configuration/),
[retention](https://prometheus.io/docs/prometheus/latest/storage/),
[alert rules](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/).
