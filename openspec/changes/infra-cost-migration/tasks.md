## 1. Preflight

- [ ] 1.1 Owner: create the Hetzner and Cloudflare accounts. Report Railway Postgres `wal_level`, the role's REPLICATION attribute, the service region and TCP-proxy reachability from Helsinki. Record the provisional plan A/plan B choice (human).
- [x] 1.4 Owner: choose the backup/DR location and the privacy-notice wording. Decided 2026-11-17 (#251): Hetzner Object Storage in Falkenstein or Nuremberg; no Contabo copy; the notice states EU storage (app in Finland, backups in Germany, served through Cloudflare).
- [ ] 1.2 Add `mise run infra:pg-replication-check`, which:
  - lists tables without a PK or replica identity, any large objects and all sequences;
  - reports publisher/subscriber LSN lag;
  - has integration tests against two compose Postgres instances.
- [ ] 1.3 Add `mise run infra:load-baseline` (k6: 1k-DAU mix plus a 10-minute SSE stream). Record the Railway baseline in `docs/infra/baseline.md`: server-side p95, plus client-side p95 from a Southeast Asian vantage point.
- [ ] 1.5 Extend `infra:pg-replication-check` with `--compare`: row counts per table and a checksum sample between two databases (plan B verify step).

## 2. Edge

- [x] 2.1 e2e: HTTP e2e test that a chat stream gets a heartbeat within 25 s.
- [x] 2.2 Integration: SSE routes in chat, courses and lessontest emit heartbeats while idle.
- [x] 2.3 Unit: cache-header policy applies only to immutable, non-personal routes.
- [x] 2.4 Implement the heartbeat and cache headers. Add Workers static-assets config for the web build.
- [ ] 2.5 Owner: Cloudflare zone and a proxied API record pointing at Railway first. Web client on Workers static assets (human).

## 3. Objects

- [ ] 3.1 Integration: `infra:s3-conformance` covers versioned put/list/get-by-version, delete-marker restore, `If-None-Match: *` 412 and retention lock. It runs against an env-provided endpoint: Hetzner Object Storage, RustFS as the fallback, and Contabo if the owner chooses it.
- [ ] 3.2 Integration: `infra:s3-copy-verify` copies all versions and delete markers and checks count, bytes, checksum sample and versions.
- [ ] 3.3 e2e/integration: the player and backup read-fallback reads the new store first and the old store second. It is config-gated and off by default.
- [ ] 3.4 Implement 3.1–3.3. Update `docs/data-inventory.md` and `infra/backup/README.md` (assets on Hetzner Object Storage in Helsinki, backups and DR in the owner-chosen location, 35-day read-only source).

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

## 5. Switchover gate

- [ ] 5.1 Integration: the gate holds new transactions for at most 15 s, waits for session advisory lock holders and soft-stops River. It swaps the pgx pool to a second database, restarts River and releases with no request errors. On a failed check it releases back to the old pool.
- [ ] 5.2 Unit: gate state machine and timeout handling. The operator-only trigger rejects non-operators.
- [ ] 5.4 Integration: in read-only mode, reads succeed, writes return `503` with `Retry-After` and River is stopped. Leaving the mode resumes writes. e2e: the client retries a progress write after `503`.
- [ ] 5.3 Implement the gate, the standby mode, the read-only mode and the sequence-sync step (`setval` from publisher values plus a margin).

## 6. Staging rehearsal (Helsinki)

- [ ] 6.1 Owner: staging VMs in `hel1`, the private network, a firewall limited to Cloudflare ranges, Object Storage buckets (Helsinki assets, backup location from 1.4) and GitHub environment secrets (human).
- [ ] 6.2 Run on staging:
  - the restore drill;
  - `engine:eval` and the live sample eval;
  - the load baseline;
  - a full rehearsal of the chosen cutover path against a Railway staging database: plan A steps 1–6 with rollback and reverse replication, or plan B with a production-sized copy in 12 minutes or less, with abort;
  - client-side latency from a Southeast Asian vantage point, recorded in `docs/infra/baseline.md`.
- [ ] 6.3 Write `docs/infra/cutover-runbook.md`: preflight, the plan A/plan B decision record, plan A steps 1–6, plan B steps, PostHog parity check, rollback and slot cleanup.

## 7. Production migration

- [ ] 7.1 Owner: provision prod VMs and secrets. Execute the runbook. Confirm PostHog parity and eval smoke at each step (human).

## 8. Decommission

- [ ] 8.1 After 30 clean days:
  - drop reverse replication;
  - remove Railway services and CI jobs;
  - archive `.railway/`;
  - amend ADR 0003 and ADR 0008;
  - archive this change.
