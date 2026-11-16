## 1. Phase 0 — measure

- [ ] 1.1 Owner exports Railway usage per service for the last 30 days (human).
- [ ] 1.2 Add a k6 load profile for 1k-DAU traffic plus a 10-minute SSE stream check; record the p95 baseline in `docs/infra/baseline.md`.

## 2. Phase 1 — edge

- [ ] 2.1 e2e: Playwright smoke against the web build served by Workers static assets (staging URL).
- [ ] 2.2 Integration: SSE routes emit a heartbeat at most every 25 s (`services/api/internal/chat`, `courses`, `lessontest` integration tests).
- [ ] 2.3 Unit: cache-header policy covers immutable routes only; authorized manifests stay `no-store`.
- [ ] 2.4 Implement the heartbeat and cache headers; add web deploy config.
- [ ] 2.5 Owner: Cloudflare account, DNS zone, proxied API record, App Sleeping on dev/staging (human).

## 3. Phase 2 — objects

- [ ] 3.1 Integration: `mise run infra:s3-conformance` exercises versioned put/list/get-by-version, delete-marker restore, `If-None-Match: *` 412, and Object Lock against RustFS and a configurable remote endpoint.
- [ ] 3.2 Implement the conformance task and a bucket copy/verify script (count, bytes, checksum sample, versions).
- [ ] 3.3 Owner: Hetzner Object Storage buckets and scoped keys; switch `S3_*`/`BACKUP_S3_*`; keep Railway buckets read-only for 35 days (human).
- [ ] 3.4 Update `docs/data-inventory.md` and `infra/backup/README.md` with the new storage location and retention.

## 4. Phase 3 — staging on VMs

- [ ] 4.1 e2e: the HTTP e2e suite runs against the compose prod overlay in CI.
- [ ] 4.2 Integration: the restore drill restores from WAL archive plus dump on a throwaway Postgres.
- [ ] 4.3 Implement the `infra/deploy` compose overlay, WAL archiving config, a host-hardening runbook, alerts (disk, WAL lag, backup age) and a VM deploy job behind `ci-ok`.
- [ ] 4.4 Owner: provision staging VM(s), private network, firewall to Cloudflare ranges, deploy credentials in GitHub environment secrets (human).
- [ ] 4.5 Run `engine:eval` and the live sample eval against the staging engine.

## 5. Phase 4 — prod cutover

- [ ] 5.1 Write the cutover and rollback runbook (freeze, dump/restore, origin flip, PostHog count parity, rollback steps).
- [ ] 5.2 Owner: approve the window, execute the cutover and confirm parity checks (human).

## 6. Phase 5 — decommission

- [ ] 6.1 After 30 clean days: remove Railway services, archive `.railway/` and update ADR 0003 and ADR 0008.
