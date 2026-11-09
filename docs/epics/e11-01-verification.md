# E11-01 verification

Ticket: E11-01, GitHub #26. Scope: encrypted nightly worker backup and synthetic
recovery against local Postgres 18 / RustFS. No cloud resources were changed.

## Acceptance evidence

- Digest-pinned pg_dump/pg_restore 18.6 and age/age-keygen 1.3.2 are in the
  non-root `worker` image. The default public `api` image stays distroless.
- River schedules 02:00 UTC and catches up on startup; date-only job args pin
  retries across midnight. Three attempts use a separate bounded backup queue.
- Only ciphertext reaches temporary storage (0600, removed on every return).
  Single conditional PutObject publishes a complete date/schema archive, with
  a 4 GiB limit and no multipart remnants. Already completed objects are skipped.
- E2e launches the actual packaged worker, decrypts its RustFS archive and
  restores to a separate synthetic database. Checks cover Goose/River metadata,
  one exact learner, two ledger entries, balance 85 and immutable ledger triggers.
- Integration injects an interrupted publication, observes River retry and one
  complete object, and repeats decrypt/restore checks. Real IAM denies backup
  deletion by writer and public-API identities and denies writer administration.
- PostHog HTTP double receives the real wrapper's success/failure events; tests
  assert reserved UUID/common properties and a closed private-data-free property
  allowlist (including documented SDK metadata). Unit tests verify opt-out and
  analytics failure independence, temporary cleanup, date/key validation,
  UTC schedule and concurrent-schema-change refusal.
- Inventory records encrypted objects, date-only queue args, temporary ciphertext
  and local IAM state. No product table, proto, AI behavior or sqlc query changed.

## TDD and commands

The new e2e test was written and run first, then integration, then unit. Each
failed before implementation (missing backup package/helper/dependency). No test
was skipped. A schema-change regression was also observed failing before its fix.

| Exact verification command | Result |
| --- | --- |
| `mise tasks` | PASS; inspected available focused tasks |
| `mise run ci:openspec` | PASS before coding and after changes; 22 validated |
| `mise run api:test-e2e` | Initial RED; final PASS, 11 tests, including packaged worker recovery |
| `mise run api:test-integration` | Initial RED; final PASS, 9 tests, Postgres/River/RustFS |
| `mise run api:test` | Initial RED; final PASS, 28 tests with race detector |
| `mise run api:lint` | PASS, 0 issues |
| `mise run api:build` | PASS, API and local initializer binaries |
| `mise run infra:lint` | PASS, Compose/Dockerfile/shell checks |
| `mise run infra:build-worker` | PASS, also runs as e2e dependency |
| `mise run infra:build` | PASS, API/worker/initializer/engine images |
| `mise run infra:test` | Initial FAIL: an unrelated local stack owned port 9000 |
| `COMPOSE_PROJECT_NAME=duolify-e11-01 POSTGRES_PORT=15432 REDIS_PORT=16379 S3_PORT=19000 S3_CONSOLE_PORT=19001 mise run infra:test` | PASS, including repeated scoped initializer execution |
| `mise exec -- git diff --check` | PASS |

Intermediate failures exposed and fixed unsupported RustFS lifecycle action
names, deprecated madmin constructor use, a one-item shell loop, SDK-added event
metadata and the numeric-user/libpq subprocess issue. The e2e retry also simulates
River cleanup so storage idempotency is tested independently of queue uniqueness.
The exploratory pinned MinIO mc image pull failed; the implementation uses the
pinned madmin API client in the development initializer instead.

## Files changed

- Runtime: `services/api/internal/backup/{backup,config,postgres,s3,worker}.go`,
  `services/api/internal/worker/worker.go`, `services/api/cmd/api/main.go`,
  `services/api/internal/config/{config,config_test}.go`.
- Local IAM: `services/api/internal/backupadmin/local.go`,
  `services/api/cmd/backup-init/main.go`.
- Tests: `services/api/test/e2e/backup_e11_01_test.go`,
  `services/api/internal/backup/back_up_a_synthetic_database_through_the_worker_test.go`,
  `services/api/internal/backup/back_up_a_synthetic_database_through_the_worker_integration_test.go`,
  `services/api/internal/backuptest/fixture.go`.
- Tooling/infrastructure: `services/api/Dockerfile`, `services/api/go.mod`,
  `services/api/go.sum`, `.mise/tasks/{api,infra}.toml`, `docker-compose.yml`,
  `infra/s3/init-buckets.sh`, `.env.example`.
- Specs/docs: `openspec/changes/worker-database-backup/{proposal,design,tasks}.md`,
  `openspec/changes/worker-database-backup/specs/data-backup/spec.md`,
  `openspec/specs/analytics/spec.md`, `docs/data-inventory.md`,
  `infra/backup/README.md`, `services/api/README.md`, `.railway/README.md`,
  `README.md`, this verification record.

## Deferred work

No E11-01 acceptance item is deferred. Existing follow-up tickets own cloud
keys/capability approval (E11-02), production 14/35-day retention and protection
(E11-03), external alert routing (E11-04), scheduled anonymized drills (E11-05),
and cloud environment/worker-stage rollout (E11-06/E11-07). Cloud workers fail
closed until their scoped settings and public recipient are provisioned.
