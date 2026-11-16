## ADDED Requirements

### Requirement: Postgres session semantics on every host
The API and worker SHALL connect to Postgres with direct or session-mode connections. A transaction-mode pooler MUST NOT be placed between them and Postgres while the code uses session-level advisory locks or River.

#### Scenario: Session advisory lock survives on the target host
- **WHEN** the integration suite runs course creation, narration and lesson test flows against the target database endpoint
- **THEN** session-level `pg_advisory_lock` and `pg_advisory_unlock` succeed on the same connection and River jobs complete

### Requirement: Object storage conformance before migration
Any object store that replaces the current one MUST pass `mise run infra:s3-conformance` before data moves. The suite SHALL cover versioned put returning a version ID, listing versions, reading by version, delete-marker restore, `If-None-Match: *` conditional create and retention locks on the deletion-ledger prefix.

#### Scenario: Unversioned store rejected
- **WHEN** the conformance suite runs against a store without bucket versioning
- **THEN** the suite fails and names the unsupported operation

### Requirement: Streaming through the edge proxy
Every SSE route SHALL send data or a comment heartbeat at least every 25 seconds, so that streams of at least 10 minutes complete through Cloudflare's proxy.

#### Scenario: Long Tutor chat stream
- **WHEN** a Tutor chat stream stays open for 10 minutes through the edge proxy
- **THEN** the client receives heartbeats and the final event without a 524 error

### Requirement: Data backup and retention across the migration
The migration SHALL preserve existing retention: general backups for 14 days, financial backups for 35 days, and the deletion ledger never expired. It SHALL add continuous WAL archiving with 7-day point-in-time recovery. Source buckets MUST stay read-only for 35 days after copy verification, and a restore drill MUST pass on the new host before production cutover.

#### Scenario: Restore drill gates cutover
- **WHEN** the restore drill fails on the target host
- **THEN** the production cutover task stays blocked and Railway remains the serving origin

### Requirement: Analytics continuity
The migration SHALL NOT add or rename PostHog events. Hourly PostHog product and LLM event counts for the 24 hours after cutover MUST stay within ±20% of the same hours in the previous week, or the cutover is rolled back.

#### Scenario: Event loss detected
- **WHEN** hourly event counts after cutover drop below the tolerance
- **THEN** the runbook triggers a rollback to the Railway origin

### Requirement: Model testing and reliability on the new host
Before production traffic moves, the Engine on the new host MUST pass `mise run engine:eval` and the live sample eval at CI thresholds. Its p95 time-to-first-token MUST be no more than 10% above the Railway baseline. Provider fallbacks, timeouts and cost limits SHALL be unchanged.

#### Scenario: Eval regression blocks cutover
- **WHEN** the live sample eval on the new host falls below CI thresholds
- **THEN** cutover is blocked and the Engine keeps serving from Railway

### Requirement: Reversible cutover
Railway SHALL stay deployable with the last known-good image digests for 30 days after cutover. Rollback SHALL be a Cloudflare origin change plus a restore of the latest dump.

#### Scenario: Rollback within 30 days
- **WHEN** the owner triggers rollback during the 30-day window
- **THEN** traffic is served by Railway again after the origin flip and data restore
