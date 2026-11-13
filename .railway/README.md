# Railway Infrastructure as Code

The [authoring file](railway.ts) owns only `duolify` / `dev` and rejects every
other target. It declares Postgres 18, Redis 8, separate lesson-asset and backup
buckets, and GitHub-backed API, worker and private engine services from `main`.
The API and worker build with `/services/api` as their root and `Dockerfile` as
their Dockerfile path. The engine uses the repository root and `engine/Dockerfile`.

```sh
mise run infra:plan
# Only after reviewing the plan and explicitly authorizing resource creation:
mise run infra:deploy-dev
```

Planning never applies or deploys. The deploy task links `duolify/dev` and invokes
interactive `railway config apply`; the apply operation can create billable
resources and deploy the configured services. Do not use this task during the
bootstrap implementation, and never target production. CI should save a plan
outside `.railway/` and apply that exact reviewed artifact, per ADR 0008.

Install the authoring SDK reproducibly with `mise exec node -- npm ci --prefix
.railway --ignore-scripts`. Both tasks do this automatically. `preserve()` retains
provider, JWT and PostHog secrets; it does not generate missing values. Supply
them in Railway before switching from mock mode or using real auth/analytics.
`ENGINE_HOST` is an IaC-only reference used to compose `ENGINE_ADDR`; it is not an
additional local runtime setting. Bucket credentials use Railway variable
references and are never committed. Both API and worker require scoped
`BACKUP_S3_*` storage for the independent deletion ledger and fail closed without
it. The scaffold preserves dedicated writer secrets supplied by owner provisioning
in E11-02/E11-07; it never substitutes the bucket's administrative keys. Deletion
objects must be excluded from archive retention and remain outside restore data.
The worker also preserves a public `BACKUP_AGE_RECIPIENT`. The cloud rollout must
select the Dockerfile `worker` stage
(pg_dump/age tools), rather than its default `api` stage. The scaffold declaration
does not enable cloud backups; finish E11-03 protection and E11-06 rollout first.

`/api migrate` is the current binary's upward-migration command. Both API and
worker run it before deploy because Railway does not guarantee service order.
The API uses HTTP `/readyz`; `/healthz` is liveness. Worker has no HTTP listener
and engine exposes gRPC health, so Railway's HTTP healthcheck path is intentionally
unset for those two. Compose probes the engine's gRPC health and the worker's
database reachability. The engine declares no public domain or TCP proxy.

See [backup follow-ups](../infra/backup/README.md) before putting real data in
these stores. In particular, bucket versioning and retention are not configured
by the region-only bucket SDK declaration.

The CLI stores directory links outside the checkout, in `~/.railway/config.json`
under `projects` keyed by directory path. That file also contains account state:
do not commit it or print it. No project IDs, credentials or local link state
belong in the authoring file.
