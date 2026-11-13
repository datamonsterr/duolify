# Design

Git-backed Railway Infrastructure as Code (`.railway/railway.ts`) defines
`dev`, `staging`, and `prod` environments for project `duolify`.

Each environment defines:
- Stateless application services (`api`, `worker`) and internal AI `engine`.
- Data stores: Postgres 18 database, Redis cache, and separate S3 buckets
  for application lesson assets and encrypted backups.
- Private networking: the engine service declares `domains: []` and binds to
  internal addresses. It is reachable by the API only through Railway private
  networking via `RAILWAY_PRIVATE_DOMAIN`. No public domain or route is assigned.
- Secret management: sensitive tokens, passwords, and provider keys use
  `preserve()` or reference syntax `${{...}}` and are never committed to git.
- Rollback and readiness: API runs `/readyz` healthcheck probing Postgres,
  Redis, S3, and Engine. Deploy runs `/api migrate` in preDeploy. Deployments
  fail closed and roll back when migrations or readiness probes fail.

The plan-only validator runs in Go (`services/api/internal/backup`) and Rust
(`engine/crates/engine-core/src/backup/`):
- Rejects configurations with non-empty public engine domains or external routing.
- Rejects configurations missing database, cache, storage, or engine dependencies.
- Rejects plaintext secret tokens or keys in configuration bodies.
- Redacts sensitive keys, tokens, and credentials in plan summaries and error output.
- Enforces release gates: any live apply operation is rejected as disabled pending
  owner human-in-the-loop approval under E11-07 (#54).

System telemetry and logs:
- Operational deploy/health logs only, with reserved operations UUID
  `00000000-0000-4000-8000-000000000011`, common properties (`source=api`, `platform=web`),
  opt-out suppression, and zero learner text.

Dependency APIs and references consulted:
- [Railway Config as Code](https://docs.railway.com/reference/config-as-code)
- [Railway Private Networking](https://docs.railway.com/guides/private-networking)
- [Railway Healthchecks](https://docs.railway.com/guides/healthchecks)
- [Go net/http](https://pkg.go.dev/net/http)
