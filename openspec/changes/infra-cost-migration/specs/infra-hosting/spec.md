## ADDED Requirements

### Requirement: Portable self-hosted stack
The full stack SHALL start with one command on any Linux host with Docker from `infra/deploy/compose.yml`: Postgres, Redis, RustFS with versioned buckets, migrations, the engine, the API, the worker and a Caddy edge that serves the web client on `/` and proxies `/v1` to the API without buffering. Only the edge MUST publish host ports; the engine MUST stay on the private network. Configuration SHALL come only from `infra/deploy/.env`, whose secrets are generated locally and never committed. With no external credentials the stack MUST run with mock providers and analytics off. The worker SHALL write the encrypted nightly backup to the local backup bucket, and `infra:selfhost-restore-drill` SHALL restore the newest backup into a throwaway, network-less Postgres and verify it.

#### Scenario: Fresh machine
- **WHEN** an operator runs `mise run infra:selfhost-up` and then `mise run infra:selfhost-smoke` on a host with only Docker and the repository
- **THEN** every service is healthy, only the proxy publishes ports, `/v1/features` answers through the proxy, the first SSE event of `/v1/stream-check` arrives within 1.5 seconds, and the web client loads with a bundle that calls the API on its own origin

#### Scenario: Backup restores on the same stack
- **WHEN** the operator runs `mise run infra:selfhost-backup` and then `mise run infra:selfhost-restore-drill`
- **THEN** today's encrypted dump exists in the backup bucket and the decrypted copy restores with applied Goose and River migrations and a valid credit ledger

### Requirement: Postgres session semantics on every host
The API and worker SHALL connect to Postgres with direct connections. A transaction-mode pooler MUST NOT be placed between them and Postgres while the code uses session-level advisory locks or River.

#### Scenario: Session advisory lock survives on the target host
- **WHEN** the integration suite runs course creation, narration and lesson test flows against the target database endpoint
- **THEN** session-level `pg_advisory_lock` and `pg_advisory_unlock` succeed on the same connection and River jobs complete

### Requirement: Object storage conformance before migration
Any object store that replaces the current one MUST pass `mise run infra:s3-conformance` before data moves. This covers RustFS in Singapore and Hetzner Object Storage in Helsinki. The suite SHALL cover:
- a versioned put that returns a version ID;
- listing versions;
- reading by version;
- delete-marker restore;
- `If-None-Match: *` conditional create;
- retention locks on the deletion-ledger prefix.

#### Scenario: Unversioned store rejected
- **WHEN** the conformance suite runs against a store without bucket versioning
- **THEN** the suite fails and names the unsupported operation

### Requirement: Object migration without unavailable reads
During object migration, the API SHALL read from the new store first and fall back to the Railway bucket when an object is missing. Writes SHALL go to the new store only after the first full copy is verified. The fallback MUST be removed after the incremental copy verifies.

#### Scenario: Object written before the incremental copy
- **WHEN** a Slide object exists only in the Railway bucket during migration
- **THEN** the Lesson still plays because the read falls back to the Railway bucket

### Requirement: Streaming through the edge proxy
Every SSE route SHALL send data or a comment heartbeat at least every 25 seconds, so that streams of at least 10 minutes complete through Cloudflare's proxy.

#### Scenario: Long Tutor chat stream
- **WHEN** a Tutor chat stream stays open for 10 minutes through the edge proxy
- **THEN** the client receives heartbeats and the final event without a 524 error

### Requirement: Zero-downtime database switchover
The database SHALL move from Railway to Hetzner by logical replication followed by a switchover gate. The gate:
- MUST hold, not reject, new database work for at most 15 seconds;
- MUST wait for in-flight transactions and session advisory lock holders;
- MUST stop River gracefully.

Before swapping the connection pool, the gate SHALL:
- set the old database read-only;
- verify subscriber LSN equals publisher LSN;
- set every sequence from the publisher plus a safety margin.

If any check fails, the gate MUST release back to the old database. Replication slots MUST be dropped at detach or rollback.

#### Scenario: Successful switchover
- **WHEN** the operator triggers the gate while replication lag is zero
- **THEN** requests resume on the Hetzner database within 15 seconds with no request errors and sequences continue above the publisher values

#### Scenario: Lag check fails
- **WHEN** the subscriber LSN does not reach the publisher LSN within the gate timeout
- **THEN** the gate clears read-only on Railway, keeps the Railway pool and releases held requests

### Requirement: Data backup and retention across the migration
The migration SHALL preserve existing retention: general backups for 14 days, financial backups for 35 days, and the deletion ledger never expired. It SHALL add:
- WAL archiving with 7-day point-in-time recovery to Hetzner Object Storage in Helsinki;
- a nightly versioned DR copy of `lesson-assets`.

Railway buckets MUST stay read-only for 35 days after verification. A restore drill MUST pass on the Singapore staging stack before production migration.

#### Scenario: Restore drill gates migration
- **WHEN** the restore drill fails on the Singapore staging stack
- **THEN** the production migration task stays blocked and Railway remains the serving origin

### Requirement: Analytics continuity
The migration SHALL NOT add or rename PostHog events. For the 24 hours after each traffic-changing step, hourly PostHog product and LLM event counts MUST stay within ±20% of the same hours in the previous week. Otherwise that step is rolled back.

#### Scenario: Event loss detected
- **WHEN** hourly event counts after a migration step drop below the tolerance
- **THEN** the runbook rolls that step back

### Requirement: Model testing and reliability on the new host
Before the database switchover, the Engine on the Singapore stack MUST:
- pass `mise run engine:eval`;
- pass the live sample eval at CI thresholds;
- keep p95 time-to-first-token no more than 10% above the Railway baseline.

Provider fallbacks, timeouts and cost limits SHALL be unchanged.

#### Scenario: Eval regression blocks switchover
- **WHEN** the live sample eval on the Singapore stack falls below CI thresholds
- **THEN** the switchover is blocked and the Engine keeps serving from Railway

### Requirement: Reversible migration
For 30 days after switchover, reverse logical replication from Hetzner to Railway SHALL keep Railway current. Rollback SHALL use the same switchover gate.

#### Scenario: Rollback within 30 days
- **WHEN** the owner triggers rollback during the 30-day window
- **THEN** the gate swaps back to Railway Postgres within 15 seconds without request errors
