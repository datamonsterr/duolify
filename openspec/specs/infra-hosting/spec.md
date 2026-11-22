# infra-hosting Specification

## Purpose
Describe the portable self-hosted stack that runs Duolify on a Linux host behind an edge proxy, including its storage, streaming, backup, analytics and reliability expectations.

## Requirements

### Requirement: Portable self-hosted stack
The full stack SHALL start with one command on any Linux host with Docker from `infra/deploy/compose.yml`: Postgres, Redis, RustFS with versioned buckets where supported (R2 is unversioned; see `data-backup` object storage protection), migrations, the engine, the API, the worker and a Caddy edge that serves the web client on `/` and proxies `/v1` to the API without buffering. Only the edge MUST publish host ports; the engine MUST stay on the private network. Configuration SHALL come only from `infra/deploy/.env`, whose secrets are generated locally and never committed. With no external credentials the stack MUST run with mock providers and analytics off. The worker SHALL write the encrypted nightly backup to the local backup bucket, and `infra:selfhost-restore-drill` SHALL restore the newest backup into a throwaway, network-less Postgres and verify it.

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
Object storage SHALL support conditional create and the backup and recovery operations required by the deployment. Versioned stores MUST support versioned puts, listing and reading versions, and delete-marker restore. R2 does not provide those version operations: backup `wal/` and `base/` use bucket lock rules and expiry, while protected asset copies are encrypted and checksum-pinned. R2 cannot recover overwritten versions or provide version-aware asset recovery or deletion-ledger versioning; these limitations require a decision before those features depend on versions.

#### Scenario: Unversioned store
- **WHEN** the deployment uses R2
- **THEN** it records the absence of versioning and uses the documented compensating controls rather than claiming version recovery

### Requirement: Streaming through the edge proxy
Every SSE route SHALL send data or a comment heartbeat at least every 25 seconds, so that streams of at least 10 minutes complete through Cloudflare's proxy (125-second Proxy Read Timeout).

#### Scenario: Long Tutor chat stream
- **WHEN** a Tutor chat stream stays open for 10 minutes through the edge proxy
- **THEN** the client receives heartbeats and the final event without a 524 error

### Requirement: Data backup and retention across the migration
The self-hosted deployment SHALL preserve general backups for 14 days, financial backups for 35 days, and the deletion ledger without expiry. WAL archiving SHALL provide seven-day point-in-time recovery and a nightly DR copy of lesson assets SHALL be stored outside the VM. Versioning applies where the store supports it; R2 uses the controls and has the limitations described in `data-backup` object storage protection.

#### Scenario: Restore drill fails
- **WHEN** a restore drill fails
- **THEN** the failure is reported and recovery must be repaired before relying on the backup

### Requirement: Analytics continuity
The migration SHALL NOT add or rename PostHog events. For the 24 hours after each traffic-changing step, hourly PostHog product and LLM event counts MUST stay within ±20% of the same hours in the previous week. Otherwise that step is rolled back.

#### Scenario: Event loss detected
- **WHEN** hourly event counts after a migration step drop below the tolerance
- **THEN** the runbook rolls that step back

### Requirement: Model testing and reliability on the new host
The Engine on the self-hosted deployment MUST pass `mise run engine:eval` and meet the live promotion evidence gate in `ai-reliability`. Provider fallbacks, timeouts and cost limits SHALL remain enforced.

#### Scenario: Eval regression blocks promotion
- **WHEN** live evaluation fails the promotion thresholds
- **THEN** live AI features remain gated until passing evidence is available

### Requirement: Off-site backups from the primary region
Backups, the WAL archive and the `lesson-assets` DR copy MUST be stored outside the primary VM, with versioning where the store supports it and the R2 compensating controls and limitations specified in `data-backup`. The storage country MUST be listed in the privacy notice before production data is written there.

#### Scenario: Backup target in the primary site rejected
- **WHEN** `infra:deploy-plan` sees a backup endpoint in the same location as the primary stack
- **THEN** the plan fails and names the backup variable

### Requirement: Latency from Southeast Asia is measured
Immutable object responses SHALL be cacheable at the edge so that repeat reads in Southeast Asia are served without an origin round trip. Client-side p95 from a Southeast Asian vantage point SHALL be measured for the current deployment.

#### Scenario: Cached slide read from Southeast Asia
- **WHEN** a learner in Southeast Asia requests an immutable Slide object that was fetched before through the same edge
- **THEN** the response is a cache hit and no request reaches the origin
