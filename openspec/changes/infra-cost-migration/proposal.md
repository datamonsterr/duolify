# Infrastructure migration to Hetzner Singapore (E18)

## Why

Owner decision 2026-11-17 (ADR 0014, Accepted): move Duolify off Railway to Hetzner Cloud VMs in Singapore, close to the Southeast Asian learner base, behind Cloudflare.

- **Stack:** the existing Docker stack (API, worker, Engine, Redis, Postgres 18), plus self-hosted RustFS for lesson assets.
- **Postgres:** self-managed, reusing `infra/backup`.
- **Cutover:** zero-downtime via logical replication.
- **Credits:** none.

The goals are:
- a predictable bill;
- a LAN database;
- egress at $8.49/TB against $50/TB;
- about 5–35% lower cost at 10k DAU.

At 100 DAU Hetzner costs more than Railway (see `docs/infra/cost-migration-options.md` §6).

## What Changes

- **Edge:** Cloudflare DNS/CDN/WAF in front of the API, and the web client on Workers static assets. SSE heartbeats keep streams under the 100 s proxy read timeout, and immutable object routes get cache headers.
- **Prod stack:** a compose overlay for Hetzner `sin` with two VMs on a private network:
  - **app VM:** API ×2, worker, Engine, Redis, RustFS on a volume;
  - **db VM:** Postgres 18 with WAL archiving to Hetzner Object Storage in Helsinki.
- **Objects:**
  - **Validation:** an S3 conformance suite runs against RustFS and Hetzner Object Storage.
  - **Copy:** a copy/verify tool moves all versions.
  - **Cutover:** temporary read-fallback to Railway buckets during the switch.
  - **DR:** a nightly versioned DR copy of `lesson-assets` goes to Helsinki.
- **Database switchover:**
  - **Gate:** a switchover gate in the API holds new database work for at most 15 s, waits for in-flight transactions and session advisory lock holders, stops River and swaps the pgx pool.
  - **Tooling:** sequence sync and replication-lag checks.
  - **Rollback:** reverse replication for 30 days.
- **Deploy:** a deploy job behind `ci-ok` on Namespace runners, deploying GHCR image digests to the VMs.
- **Runbooks:** preflight, migration and rollback.
- **Decommission:** Railway is removed after 30 clean days.
- No proto change and no learner-facing behaviour change.

## Impact

- **client:** the web build is deployed to Workers static assets. The API domain is unchanged.
- **api:**
  - SSE heartbeat and cache-header policy;
  - S3 read-fallback, behind configuration and temporary;
  - the database switchover gate (pool swap, River stop/start);
  - an admin trigger restricted to operators.

  Advisory locks, River and `pgx` direct connections are kept, with no transaction pooler.
- **engine:** same image on the private network. Evals must pass on the new host.
- **proto:** none.
- **infra:**
  - `infra/deploy` overlay, Postgres and WAL configuration, RustFS volume;
  - mise tasks: `infra:s3-conformance`, `infra:s3-copy-verify`, `infra:pg-replication-check`, `infra:deploy-plan`, `infra:load-baseline`;
  - runbooks and alerts.
- **ADRs:** 0014 (Accepted). 0003 and 0008 are amended at decommission. 0002 is kept.
- **Owner gates (ready-for-human):**
  - Hetzner and Cloudflare accounts, DNS and secrets;
  - Railway `wal_level`/role/region preflight;
  - executing the migration runbook.
