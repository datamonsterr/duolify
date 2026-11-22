# Infrastructure migration to Hetzner Helsinki (E18)

## Why

Owner decisions of 2026-11-17 (ADR 0014, Accepted; revised the same day from Singapore to Helsinki) move Duolify off Railway to Hetzner Cloud VMs in Helsinki (`hel1`), behind Cloudflare:

- **Stack:** the existing Docker stack (API, worker, Engine, Redis, Postgres 18).
- **Objects:** lesson assets on Hetzner Object Storage in Helsinki.
- **Postgres:** self-managed, reusing `infra/backup`.
- **Cutover:** zero-downtime logical replication as plan A. If Railway cannot run `wal_level=logical`, a planned read-only window of at most 15 minutes is the fallback.
- **Credits:** none.

Helsinki is Hetzner's cheapest location. Estimated prod cost is ~$28, ~$44–87 and ~$234 per month at 100, 1k and 10k DAU, against Railway's ~$20, ~$70 and ~$310–450. The Singapore plan cost ~$39, ~$66–129 and ~$291. Other gains:
- a predictable bill;
- a LAN database;
- 20 TB of included traffic per VM.

The owner accepts about +175 ms per uncached round trip for Southeast Asian learners. Cloudflare serves cached objects from local edges. See `docs/infra/cost-migration-options.md` §10.

## What Changes

- **Edge:** Cloudflare DNS/CDN/WAF and Tiered Cache in front of the API, and the web client on Workers static assets. SSE heartbeats keep streams under the 125 s proxy read timeout, and immutable object routes get cache headers.
- **Prod stack:** a compose overlay for Hetzner `hel1` with two VMs on a private network:
  - **app VM:** API ×2, worker, Engine, Redis;
  - **db VM:** Postgres 18 with WAL archiving to the backup bucket.
- **Objects:**
  - **Primary:** `lesson-assets` on Hetzner Object Storage in Helsinki.
  - **Validation:** an S3 conformance suite must pass on every target store. RustFS on a Volume is the fallback if Hetzner Object Storage fails it.
  - **Copy:** a copy/verify tool moves all versions.
  - **Cutover:** temporary read-fallback to the Railway buckets.
  - **Backup/DR location (owner, 2026-11-17):** Hetzner Object Storage in Falkenstein or Nuremberg for backups, the WAL archive and a nightly versioned DR copy of `lesson-assets`. There is no Contabo copy.
- **Database switchover:**
  - **Gate:** a switchover gate in the API holds new database work for at most 15 s, waits for in-flight transactions and session advisory lock holders, stops River and swaps the pgx pool. Compute and the database move together.
  - **Read-only mode:** the gate also has a read-only mode for the fallback. Reads continue, and writes get `503` with `Retry-After`.
  - **Tooling:** sequence sync, replication-lag and row-count checks.
  - **Rollback:** reverse replication for 30 days (plan A), or a reverse dump/restore window (plan B).
- **Deploy:** a deploy job behind `ci-ok` on Namespace runners, deploying GHCR image digests to the VMs.
- **Runbooks:** preflight, the plan A/plan B decision, migration and rollback.
- **Decommission:** Railway is removed after 30 clean days.
- No proto change. No learner-facing change beyond added latency on uncached requests, and the client "saving paused" retry if plan B runs.

## Impact

- **client:** the web build is deployed to Workers static assets. The API domain is unchanged. Writes retry on `503` with `Retry-After` (plan B only).
- **api:**
  - SSE heartbeat and cache-header policy;
  - S3 read-fallback, behind configuration and temporary;
  - the database switchover gate (pool swap, River stop/start, read-only mode);
  - an admin trigger restricted to operators.

  Advisory locks, River and `pgx` direct connections are kept, with no transaction pooler.
- **engine:** same image on the private network. Evals must pass on the new host.
- **proto:** none.
- **infra:**
  - `infra/deploy` overlay, Postgres and WAL configuration;
  - mise tasks: `infra:s3-conformance`, `infra:s3-copy-verify`, `infra:deploy-plan`, `infra:load-baseline`;
  - runbooks and alerts.
- **ADRs:** 0014 (Accepted). 0003 and 0008 are amended at decommission. 0002 is kept.
- **Owner gates (ready-for-human):**
  - Hetzner and Cloudflare accounts, DNS and secrets;
  - Railway `wal_level`/role/region preflight;
  - executing the migration runbook.
