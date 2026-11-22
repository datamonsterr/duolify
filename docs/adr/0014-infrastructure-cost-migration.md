# ADR 0014: Infrastructure migration off Railway to Hetzner Helsinki

- Status: Accepted (owner decisions 2026-11-17, revised the same day from Singapore to Helsinki; backup/DR location and privacy wording decided later on 2026-11-17, [#251](https://github.com/datamonsterr/duolify/issues/251))
- Date: 2026-11-17
- Evidence: [docs/infra/cost-migration-options.md](../infra/cost-migration-options.md). §10 holds the Helsinki, latency and Contabo analysis; §5–§9 hold the superseded Singapore plan. Prices were read 2026-11-16 (comparison) and 2026-11-17 (Singapore, Helsinki, Contabo, cutover).
- Relates to: [0002](0002-service-architecture.md) (kept), [0003](0003-data-stores.md) and [0008](0008-ci-and-deploy.md) (amended after decommission)

## Context

Railway bills used CPU and RAM plus $0.05/GB service egress, and slides and audio stream through the API. Most learners are in Southeast Asia. Hetzner Singapore costs 1.5–1.6× Helsinki and has no Object Storage, so the first plan (Singapore) cost more than Railway at 100 DAU. The code sets these constraints:

- **S3 bucket versioning is required.** Cloudflare R2 lacks it.
- **Session-level advisory locks and River need direct Postgres.** Transaction poolers and Hyperdrive are out.
- **The worker must run all the time.**
- **SSE needs heartbeats** under Cloudflare's 125 s Proxy Read Timeout.

## Decision

1. **Option C in Hetzner Helsinki (`hel1`, owner 2026-11-17).** Hetzner Cloud VMs run the existing Docker stack: API, worker, engine, Redis and Postgres 18. Cloudflare sits in front (DNS, CDN, WAF, Tiered Cache) and serves the web client from Workers static assets, so cached slides, audio and static files reach Southeast Asia from local edges. The owner accepts the extra latency on uncached requests (about +175 ms per round trip, §10.5).
2. **Lesson assets on Hetzner Object Storage in Helsinki.** It is versioned and Object Lock capable, cheaper than RustFS on a Volume at every load level (the €6.49 base is paid anyway, and VM↔bucket traffic in `eu-central` is free), and it must pass `infra:s3-conformance`. RustFS on a Volume stays the fallback if the suite fails.
3. **Backups and DR on Hetzner Object Storage in Falkenstein or Nuremberg (owner, 2026-11-17).** This holds `pg_dump`, the WAL archive, the deletion ledger (retention-locked) and a nightly versioned copy of `lesson-assets`, all off-site from Helsinki. There is **no** third copy on Contabo. Contabo is not used for hosting or backups (§10.7–§10.8).
4. **Postgres is self-managed on a VM.** It uses `infra/backup` (encrypted dumps, restore drills) plus WAL archiving for PITR.
5. **Cutover.**
   - **Plan A, zero-downtime.** Logical replication runs from Railway to Hetzner. A request-holding switchover gate pauses writes for at most 15 s while sequences sync, then compute and the database move together. Reverse replication keeps rollback open for 30 days.
   - **Plan B, owner-accepted fallback.** If Railway cannot run with `wal_level=logical`, use a planned read-only window of at most 15 minutes: reads stay up, writes return 503 with `Retry-After`, and the data moves by parallel dump/restore.
   - **Decision point.** E18-01 preflight gives a provisional choice. The E18-10 staging rehearsal confirms it (§10.9).
6. **No startup credits.**
7. **Argo Smart Routing is decided after the staging latency data** (E18-10).

## Consequences

- **Cost against Railway (USD, excl. VAT):**

  | DAU | Hetzner Helsinki | Railway |
  |---|---|---|
  | 100 | ~$28 | ~$20 |
  | 1k | ~$44–87 | ~$70 |
  | 10k | ~$234 | ~$310–450 |

  The Singapore plan was ~$39, ~$66–129 and ~$291 respectively, before volume costs. Origin traffic stays inside the 20 TB per VM included in the EU, then costs €1/TB.
- **Latency.** Uncached API calls and SSE first bytes from Southeast Asia gain about 170–210 ms per round trip (WonderNetwork medians, 2026-11-02 to 2026-11-16). Cached immutable objects are unaffected, and a cache miss pays the trip once per object. Server-side p95 is gated against the Railway baseline, and the client-side increase is recorded, not gated.
- **Operations.** We own OS patching, Postgres upgrades, WAL/PITR and the replication-slot hygiene of the cutover.
- **Railway Postgres.** Plan A may need a one-time `wal_level=logical` restart. Railway does not support or document this setting. If that is refused or does not stick, Plan B applies.
- **Residency.** All learner data is stored in the EU: the app in Finland (Helsinki) and backups in Germany (Falkenstein or Nuremberg). It is served through Cloudflare. The owner approved this wording for the privacy notice on 2026-11-17; the data-inventory/privacy update is part of E18-06.
- **New code:**
  - SSE heartbeats and immutable cache headers
  - S3 read-fallback during object migration
  - the DB switchover gate, with a read-only mode for Plan B
  - the S3 conformance and copy/verify tasks
  - the deploy job
- **ADRs.** ADR 0003 and ADR 0008 are amended when Railway is decommissioned.

## Amendment 2026-11-22: Cloudflare R2, domain, no Railway migration (owner)

- **Object storage is Cloudflare R2** (S3-compatible, `https://<account>.r2.cloudflarestorage.com`, region `auto`) for the application bucket and for backups, WAL and the DR copy; not Hetzner Object Storage. The compute stays on Hetzner `hel1`, so `BACKUP_LOCATION` is `cloudflare-r2`, outside the VM. The overlay and env templates use generic `S3_*` / `BACKUP_S3_*` variables (`S3_ADDRESSING_STYLE`, `BACKUP_S3_VERSIONING`).
- **R2 has no bucket versioning** (S3 `PutBucketVersioning` is unimplemented). Consequences: the backup lifecycle is expiry-only (`BACKUP_S3_VERSIONING=none`), `wal/` and `base/` are protected by R2 bucket lock rules (owner step, not settable via S3), and the DR copy cannot keep overwritten versions. Features that need object versions (point 1 above, version-aware asset recovery and the deletion ledger) are not available on R2 and need a decision before real assets go there.
- **Domain:** `duolify.online`, Cloudflare in front.
- **No migration from Railway** (D1): the Hetzner stack is a fresh deploy; the logical-replication cutover phases are not executed.
