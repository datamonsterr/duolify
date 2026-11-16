# ADR 0014: Infrastructure migration off Railway to Hetzner Singapore

- Status: Accepted (owner decisions 2026-11-17; options proposed 2026-11-16)
- Date: 2026-11-17
- Evidence: [docs/infra/cost-migration-options.md](../infra/cost-migration-options.md). Prices were read 2026-11-16 (comparison) and 2026-11-17 (Singapore and cutover).
- Relates to: [0002](0002-service-architecture.md) (kept), [0003](0003-data-stores.md) and [0008](0008-ci-and-deploy.md) (amended after decommission)

## Context

Railway bills used CPU and RAM plus $0.05/GB service egress, and slides and audio stream through the API. Most learners are in Southeast Asia, and Railway's buckets already sit in `sin`. The code sets the following constraints:

- **S3 bucket versioning is required.** Cloudflare R2 lacks it.
- **Session-level advisory locks and River need direct Postgres.** Transaction poolers and Hyperdrive are out.
- **The worker must run all the time.**
- **SSE needs heartbeats** under Cloudflare's 100 s proxy read timeout.

## Decision (owner, 2026-11-17)

1. **Option C now.** Hetzner Cloud VMs in **Singapore (`sin`)** run the existing Docker stack: API, worker, engine, Redis, Postgres 18 and RustFS. Cloudflare sits in front: DNS, CDN, WAF, and the web client on Workers static assets. There is no Railway-trimming phase.
2. **Objects.** Hetzner Object Storage is not offered in Singapore, so:
   - `lesson-assets` runs on self-hosted RustFS on a Singapore volume.
   - `backups`, the WAL archive and a nightly versioned DR copy of assets go to Hetzner Object Storage in Helsinki.
3. **Postgres is self-managed on a VM.** It uses `infra/backup` (encrypted dumps, restore drills) plus WAL archiving for PITR.
4. **Zero-downtime cutover.** Logical replication runs from Railway to Hetzner. Compute moves first while still pointing at the Railway DB. A request-holding switchover gate then pauses writes for at most 15 s while sequences sync and the DB pool swaps. Reverse replication keeps rollback possible for 30 days. There is no write freeze or maintenance window.
5. **No startup credits for now.**

## Consequences

- **Cost.** Singapore prices are about 1.5–1.6× Helsinki. Estimates against Railway:
  - 100 DAU: ~$39/month, more than Railway's ~$20.
  - 1k DAU: ~$66–129, against ~$70.
  - 10k DAU: ~$291, against ~$310–450.

  All Hetzner figures exclude volume price, which still needs checking. The gains are a predictable bill, a LAN database, and egress at $8.49/TB instead of $50/TB, more than a lower bill at small scale.
- **Operations.** We own OS patching, Postgres upgrades, WAL/PITR, RustFS durability, and the replication-slot hygiene of the cutover.
- **Railway Postgres.** Logical replication may need a one-time `wal_level=logical` restart of Railway Postgres, a blip of seconds. Railway does not support or document this setting.
- **Residency.** Backups are stored in the EU. This needs a privacy-notice entry.
- **New code.** It is limited to:
  - SSE heartbeats and immutable cache headers
  - S3 read-fallback during object migration
  - the DB switchover gate
  - the S3 conformance and copy/verify tasks
  - the deploy job
- **ADRs.** ADR 0003 and ADR 0008 are amended when Railway is decommissioned.
