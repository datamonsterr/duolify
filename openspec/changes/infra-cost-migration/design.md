# Design: migration to Hetzner Helsinki

Source of truth for prices, limits and facts: `docs/infra/cost-migration-options.md` §10 (read 2026-11-17); §5–§9 hold the superseded Singapore plan.

## Topology

```text
learner (SEA) ─▶ Cloudflare (DNS, CDN, WAF, Tiered Cache; web client on Workers static assets; SEA edges)
                   │ proxied HTTPS over warm connections (~175 ms SEA→HEL); origin firewall: Cloudflare ranges only
                   ▼
   Hetzner hel1, private network (eu-central)
   ├─ app VM: api serve ×2, worker (River), engine (gRPC, private), redis
   └─ db VM:  postgres 18 (local NVMe), WAL archive
                   │ free eu-central traffic
                   ▼
   Hetzner Object Storage hel1: lesson-assets (versioned)
   Hetzner Object Storage fsn1/nbg1 (pending owner): backups (versioned; deletion ledger retention-locked),
                                                     WAL archive, lesson-assets DR copy (versioned)
   optional (pending owner): Contabo Object Storage EU, third copy of encrypted dumps
```

## Key decisions

- **Direct Postgres connections.** The session-level `pg_advisory_lock` calls (lessontest, course creation, narration) and River need session semantics, so no transaction pooler is used.
- **Hetzner Object Storage for `lesson-assets` in Helsinki.** It is versioned and Object Lock capable, VM↔bucket traffic in `eu-central` is free, and it is cheaper than a Volume at every load level (€0 marginal up to 1 TB, ~€12.5 at 3 TB against €132 on a Volume). `infra:s3-conformance` must pass first. If it fails, RustFS on a Volume, already the local store, is the fallback.
- **Backups off-site from Helsinki (pending owner).** The recommendation is a second Hetzner EU location (Falkenstein or Nuremberg). It holds `pg_dump` (age-encrypted), the WAL archive, the deletion ledger and the DR copy. It shares the account's base price, has free transfer and restores at Gbit speed. Contabo Object Storage (80 Mbit/s cap) is only considered for an optional provider-independent third copy of dumps.
- **Latency.** Uncached requests from Southeast Asia gain about 175 ms per round trip (WonderNetwork, 2026-11-02 to 2026-11-16). Cached immutable objects are served from Southeast Asian edges. Server-side p95 is gated, and the client-side p95 from Southeast Asia is recorded.
- **Engine stays private.** gRPC runs over the private network and the Engine is never proxied by Cloudflare.
- **SSE through Cloudflare.** A comment heartbeat is sent at most every 25 s on every SSE route, well under Cloudflare's 125 s Proxy Read Timeout.
- **Caching.** Only immutable, non-personal object responses are `public, max-age=31536000, immutable`. Authorized manifests stay `private, no-store`.

## Cutover plan A: zero-downtime

1. **Preflight** (owner + agent): check `wal_level`, the REPLICATION role and the Railway region, and audit for tables without a PK/replica identity and for large objects. If `wal_level` is not `logical`, schedule one Railway Postgres restart in the lowest-traffic minute. It costs seconds of reconnects, and pgx retries.
2. **Objects:** copy all versions, then enable read-fallback (new store first, Railway bucket second). Switch writes with a rolling deploy, then run an incremental copy and verify. Remove the fallback.
3. **Replicate:** copy the schema with `pg_dump --schema-only`, then create the publication on Railway (TLS through the TCP proxy) and a subscription on Hetzner with `copy_data`. Migrations are frozen until step 6.
4. **Standby compute:** the Hetzner app stack is deployed in standby: gate closed, River off, pointed at the Hetzner subscriber database. Railway compute keeps serving. Railway Postgres is not used from Helsinki outside the switchover, because a cross-region database would add ~100–175 ms per query. If preflight shows Railway in the EU, compute may move first instead.
5. **Switchover gate:**
   1. Hold new DB work for at most 15 s; the target is under 5 s.
   2. Wait for in-flight transactions and session-lock holders, bounded.
   3. Soft-stop River.
   4. Set Railway `default_transaction_read_only = on`, terminate the remaining sessions, and wait for the subscriber LSN to equal the publisher LSN.
   5. `setval` every sequence with a margin.
   6. Open the Hetzner gate and start River, then switch the Cloudflare origin to Hetzner.
   7. The Railway API swaps its pool to the Hetzner database, so held requests complete. Then scale Railway compute to zero.

   If any check fails, the gate releases back to Railway: clear `read_only` and keep the old pool.
6. **Detach:** drop the subscription and the Railway slot, and unfreeze migrations. Set up reverse replication from Hetzner to Railway for the 30-day rollback window.

## Cutover plan B: read-only window of at most 15 minutes (fallback)

Plan B is used if Railway cannot run `wal_level=logical`, or if the plan A rehearsal fails twice.

1. **Rehearse** on Helsinki staging with a production-sized copy. It must finish in 12 minutes or less.
2. **Read-only mode:** the gate enters read-only mode on the Railway API. Reads and playback continue. Writes return `503` with `Retry-After`, and the client retries. River soft-stops, and Railway is set to `default_transaction_read_only`.
3. **Copy:** parallel `pg_dump -Fd` → `pg_restore`. Sequences travel with the dump.
4. **Verify:** row counts and a checksum sample are checked, then `ANALYZE`.
5. **Switch:** open the Hetzner stack, switch the Cloudflare origin and scale Railway compute to zero.
6. **Abort:** if verification has not passed by minute 13, clear read-only and stay on Railway.
7. **Rollback within 30 days:** a reverse dump/restore window.

**Decision point:**
- **Provisional, after E18-01:** plan A if `wal_level` is `logical` (or the owner accepts one restart that persists), the role can replicate and publish, the TCP proxy is reachable from Helsinki, and the PK audit is clean.
- **Final, after the E18-10 rehearsal.**

Redis is derived data, so it is not migrated. It starts cold, and every key family must rebuild from Postgres; a test proves this.

## Data backup and retention

- No new learner data is created. Retention is unchanged: general backups 14 days, financial backups 35 days, and the deletion ledger is never expired.
- New:
  - WAL archiving with 7-day PITR.
  - A nightly versioned DR copy of `lesson-assets` in the backup location (off-site from Helsinki).
  - Railway buckets are kept read-only for 35 days after verification.
  - Replication slots are always dropped at detach or rollback.
- The restore drill runs on the Helsinki staging stack before prod migration and on the existing schedule afterwards.
- Residency: primary data in Finland and backups in Germany (recommended) keep all learner data in the EU. The privacy notice must state the transfer from learners' countries to the EU. Contabo Singapore for backups would add Singapore.

## Analytics

- No new product events. PostHog product and LLM event hourly counts must stay within ±20% of the same hours a week earlier for 24 h after each step that changes traffic (standby, switchover and detach; plan B steps 2–5).
- Prometheus alerts cover: disk, WAL-archive lag, backup age, replication lag, bucket usage and DR-copy age.

## Model testing and reliability

- Engine code, provider routing, fallbacks, timeouts and cost limits are unchanged.
- Before the switchover, the Engine on Helsinki staging must pass:
  - `mise run engine:eval`;
  - the live sample eval at CI thresholds;
  - p95 time-to-first-token, measured at the origin, no more than 10% above the Railway baseline.

## Risks

- **Ops burden** (Postgres, patching): runbooks and alerts. Rehearse steps 1–6 on staging before prod.
- **Railway may refuse or reset `wal_level`, since it is unsupported:** plan B, owner-accepted (a read-only window of at most 15 minutes).
- **Railway far from Helsinki:** compute and the database move together inside the gate window.
- **Southeast Asian latency hurts engagement:** measure on staging. Mitigations are caching, call collapsing and Argo. Contabo Singapore is the documented re-plan option.
- **Hetzner Object Storage fails conformance:** RustFS on a Volume.
