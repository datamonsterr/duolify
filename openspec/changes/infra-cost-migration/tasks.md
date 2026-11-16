## 1. Preflight

- [ ] 1.1 Owner: create the Hetzner and Cloudflare accounts. Report Railway Postgres `wal_level`, the role's REPLICATION attribute and the service region (human).
- [ ] 1.2 Add `mise run infra:pg-replication-check`, which:
  - lists tables without a PK or replica identity, any large objects and all sequences;
  - reports publisher/subscriber LSN lag;
  - has integration tests against two compose Postgres instances.
- [ ] 1.3 Add `mise run infra:load-baseline` (k6: 1k-DAU mix plus a 10-minute SSE stream). Record the Railway baseline in `docs/infra/baseline.md`.

## 2. Edge

- [ ] 2.1 e2e: HTTP e2e test that a chat stream gets a heartbeat within 25 s.
- [ ] 2.2 Integration: SSE routes in chat, courses and lessontest emit heartbeats while idle.
- [ ] 2.3 Unit: cache-header policy applies only to immutable, non-personal routes.
- [ ] 2.4 Implement the heartbeat and cache headers. Add Workers static-assets config for the web build.
- [ ] 2.5 Owner: Cloudflare zone and a proxied API record pointing at Railway first. Web client on Workers static assets (human).

## 3. Objects

- [ ] 3.1 Integration: `infra:s3-conformance` covers versioned put/list/get-by-version, delete-marker restore, `If-None-Match: *` 412 and retention lock. It runs against RustFS and an env-provided endpoint.
- [ ] 3.2 Integration: `infra:s3-copy-verify` copies all versions and delete markers and checks count, bytes, checksum sample and versions.
- [ ] 3.3 e2e/integration: the player and backup read-fallback reads the new store first and the old store second. It is config-gated and off by default.
- [ ] 3.4 Implement 3.1–3.3. Update `docs/data-inventory.md` and `infra/backup/README.md` (RustFS in Singapore, Helsinki DR and backups, 35-day read-only source).

## 4. Prod stack and deploy

- [ ] 4.1 e2e: the HTTP e2e suite runs against the `infra/deploy` overlay (RustFS, Postgres with WAL archiving) in CI.
- [ ] 4.2 Integration: the restore drill does PITR from WAL archive plus dump on a throwaway Postgres. A test proves the Redis key families rebuild from Postgres.
- [ ] 4.3 Unit: `infra:deploy-plan` rejects a public Engine port, a transaction pooler, a missing backup env or a missing DR-copy job.
- [ ] 4.4 Implement:
  - the overlay, WAL archiving and the nightly DR copy;
  - alerts for disk, WAL lag, backup age, replication lag and RustFS usage;
  - a hardening runbook;
  - a deploy job behind `ci-ok` on Namespace runners.

## 5. Switchover gate

- [ ] 5.1 Integration: the gate holds new transactions for at most 15 s, waits for session advisory lock holders and soft-stops River. It swaps the pgx pool to a second database, restarts River and releases with no request errors. On a failed check it releases back to the old pool.
- [ ] 5.2 Unit: gate state machine and timeout handling. The operator-only trigger rejects non-operators.
- [ ] 5.3 Implement the gate and the sequence-sync step (`setval` from publisher values plus a margin).

## 6. Staging rehearsal (Singapore)

- [ ] 6.1 Owner: staging VMs in `sin`, the private network, a firewall limited to Cloudflare ranges, a Helsinki Object Storage bucket and GitHub environment secrets (human).
- [ ] 6.2 Run on staging:
  - the restore drill;
  - `engine:eval` and the live sample eval;
  - the load baseline;
  - a full rehearsal of cutover steps 1–6 against a Railway staging database, including rollback and reverse replication.
- [ ] 6.3 Write `docs/infra/cutover-runbook.md`: preflight, steps 1–6, PostHog parity check, rollback and slot cleanup.

## 7. Production migration

- [ ] 7.1 Owner: provision prod VMs and secrets. Execute the runbook. Confirm PostHog parity and eval smoke at each step (human).

## 8. Decommission

- [ ] 8.1 After 30 clean days:
  - drop reverse replication;
  - remove Railway services and CI jobs;
  - archive `.railway/`;
  - amend ADR 0003 and ADR 0008;
  - archive this change.
