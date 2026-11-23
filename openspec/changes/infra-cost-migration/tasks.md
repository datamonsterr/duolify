## 1. Preflight

- [~] 1.1 Obsolete in part (ADR 0015 D1: fresh deploy, nothing to migrate from Railway). The Hetzner and Cloudflare accounts exist (staging live since 2026-11-22). The Railway Postgres facts and the plan A/plan B choice are dropped.

- [x] 1.4 Owner: choose the backup/DR location and the privacy-notice wording. Decided 2026-11-17 (#251) for Hetzner Object Storage; superseded by ADR 0015: Cloudflare R2 holds app assets and backups (EU jurisdiction wording to be confirmed with the notice).

- [~] 1.2 Dropped (owner decision D1, 2026-11-22: nothing to migrate from Railway, Duolify deploys fresh on Hetzner). The logical-replication check was not merged.
- [x] 1.3 Add `mise run infra:load-baseline` (k6 1k-DAU read mix, public and authenticated, plus a 10-minute Tutor chat SSE stream through the API with a mock Engine behind it; heartbeat gap must stay under 25 s). Record the Railway baseline in `docs/infra/baseline.md`: server-side p95, plus client-side p95 from a Southeast Asian vantage point.
- [~] 1.5 Dropped with 1.2 (D1).

## 2. Edge

- [x] 2.1 e2e: HTTP e2e test that a chat stream gets a heartbeat within 25 s.
- [x] 2.2 Integration: SSE routes in chat, courses and lessontest emit heartbeats while idle.
- [x] 2.3 Unit: cache-header policy applies only to immutable, non-personal routes.
- [x] 2.4 Implement the heartbeat and cache headers. Add Workers static-assets config for the web build.
- [~] 2.5 Obsolete (ADR 0015 D1: no Railway origin to proxy). Cloudflare zone `duolify.online`, proxied records and the web client are provisioned for staging (`docs/infra/staging.md`); production DNS is part of 7.1.

## 3. Objects

- [ ] 3.1 Integration: `infra:s3-conformance` covers put/list/get, `If-None-Match: *` 412 and the delete behaviour that applies to Cloudflare R2 (R2 has no bucket versioning, so versioned put/list/get-by-version and retention lock are checked only on RustFS). It runs against an env-provided endpoint: R2 first, RustFS as the local fallback (draft PR #260). Hetzner Object Storage and Contabo are dropped (ADR 0015).

- [~] 3.2 Obsolete (ADR 0015 D1): no copy from an old store, there is no old store to copy from.

- [~] 3.3 Obsolete (ADR 0015 D1): no old-store read fallback.

- [x] 3.4 Update `docs/data-inventory.md` and `infra/backup/README.md` for the real layout: assets and backups on Cloudflare R2 (`duolify-assets`, `duolify-backups`), compute on Hetzner `fsn1` (staging), no Railway storage or 35-day read-only source.

## 4. Prod stack and deploy

- [x] 4.0 Portable self-hosted stack (`infra/deploy`, `infra:selfhost-*`): one-command compose stack with a Caddy edge, generated secrets, local encrypted backups and a restore drill; CI `selfhost` job runs smoke, backup and restore drill. WAL archiving, the DR copy and the VM deploy job stay in 4.4.
- [x] 4.1 e2e: the HTTP e2e suite runs against the `infra/deploy` overlay (S3 endpoint from env, Postgres with WAL archiving) in CI.
- [x] 4.2 Integration: the restore drill does PITR from WAL archive plus dump on a throwaway Postgres. A test proves the Redis key families rebuild from Postgres.
- [x] 4.3 Unit: `infra:deploy-plan` rejects a public Engine port, a transaction pooler, a missing backup env or a missing DR-copy job.
- [x] 4.4 Implement:
  - the overlay, WAL archiving and the nightly DR copy;
  - alerts for disk, WAL lag, backup age, replication lag and bucket usage;
  - a hardening runbook;
  - a deploy job behind `ci-ok` on Namespace runners.

## 5. Switchover gate (obsolete)

- [~] 5.1 Obsolete (ADR 0015 D1): no database switchover gate between two live databases.

- [~] 5.2 Obsolete (ADR 0015 D1): no switchover gate state machine.

- [~] 5.4 Obsolete (ADR 0015 D1): no migration read-only mode.

- [~] 5.3 Obsolete (ADR 0015 D1): no gate, standby mode or sequence sync.

## 6. Staging

- [x] 6.1 Owner: staging provisioned 2026-11-22 on one Hetzner cx23 in `fsn1` (not `hel1`: no affordable x86 type was in stock, see `docs/infra/staging.md`), firewall limited to Cloudflare ranges plus key-only SSH, R2 buckets, GitHub environment `staging` secrets. Live at staging.duolify.online.

- [ ] 6.2 Run on staging (fresh deploy, no cutover rehearsal; ADR 0015 D1):
  - the restore drill and the PITR drill;
  - `engine:eval` and the live sample eval;
  - the load baseline;
  - client-side latency from a Southeast Asian vantage point, recorded in `docs/infra/baseline.md`.

- [x] 6.3 Write `docs/infra/cutover-runbook.md` as the fresh-deploy and disaster-recovery runbook (preflight, deploy, verify, PostHog parity check, rollback, restore).

## 7. Production deploy

- [ ] 7.1 Owner: provision the production VM, R2 buckets and secrets (location open, see `docs/infra/staging.md`). Run the fresh-deploy runbook and confirm PostHog parity and eval smoke (human).

## 8. Decommission

- [ ] 8.1 After production is live and 30 clean days (gated on #242; no reverse replication exists):
  - remove Railway services and CI jobs;
  - archive `.railway/`;
  - amend ADR 0003 and ADR 0008;
  - archive this change.
