# Design: migration to Hetzner Singapore

Source of truth for prices, limits and facts: `docs/infra/cost-migration-options.md` §5–§9 (read 2026-11-16/17).

## Topology

```text
learner (SEA) ─▶ Cloudflare (DNS, CDN, WAF; web client on Workers static assets)
                   │ proxied HTTPS; origin firewall: Cloudflare ranges only
                   ▼
   Hetzner sin, private network (ap-southeast)
   ├─ app VM: api serve ×2, worker (River), engine (gRPC, private), redis, rustfs (+ volume)
   └─ db VM:  postgres 18 (local NVMe), WAL archive
                   ▼
   Hetzner Object Storage hel1: backups (versioned; deletion ledger retention-locked),
                                lesson-assets DR copy (versioned)
```

## Key decisions

- **Direct Postgres connections.** The session-level `pg_advisory_lock` calls (lessontest, course creation, narration) and River need session semantics, so no transaction pooler is used.
- **RustFS for `lesson-assets` in Singapore.** Hetzner Object Storage is not offered in `sin`. RustFS is already the local store, and the backup/player integration tests already exercise versioning, delete-marker restore and `If-None-Match` against it. `infra:s3-conformance` re-proves this on the target. Durability comes from the volume plus a nightly versioned copy to Helsinki.
- **Backups off-site in Helsinki.** This covers `pg_dump` (age-encrypted), the WAL archive and the deletion ledger. Latency there is irrelevant.
- **Engine stays private.** gRPC runs over the private network and the Engine is never proxied by Cloudflare.
- **SSE through Cloudflare.** A comment heartbeat is sent at most every 25 s on every SSE route.
- **Caching.** Only immutable, non-personal object responses are `public, max-age=31536000, immutable`. Authorized manifests stay `private, no-store`.

## Zero-downtime cutover

1. **Preflight** (owner + agent): check `wal_level`, the REPLICATION role and the Railway region, and audit for tables without a PK/replica identity and for large objects. If `wal_level` is not `logical`, schedule one Railway Postgres restart in the lowest-traffic minute. It costs seconds of reconnects, and pgx retries.
2. **Objects:** copy all versions, then enable read-fallback (new store first, Railway bucket second). Switch writes with a rolling deploy, then run an incremental copy and verify. Remove the fallback.
3. **Replicate:** copy the schema with `pg_dump --schema-only`, then create the publication on Railway (TLS through the TCP proxy) and a subscription on Hetzner with `copy_data`. Migrations are frozen until step 6.
4. **Shift compute:** the Hetzner app stack uses Railway Postgres, a single database, so there is no split brain and River keeps one leader. Shift the Cloudflare origin, then scale Railway compute to zero.
5. **Switchover gate:**
   1. Hold new DB work for at most 15 s; the target is under 5 s.
   2. Wait for in-flight transactions and session-lock holders, bounded.
   3. Soft-stop River.
   4. Set Railway `default_transaction_read_only = on`, terminate the remaining sessions, and wait for the subscriber LSN to equal the publisher LSN.
   5. `setval` every sequence with a margin.
   6. Swap the pool, start River and release the gate.

   If any check fails, the gate releases back to Railway: clear `read_only` and keep the old pool.
6. **Detach:** drop the subscription and the Railway slot, and unfreeze migrations. Set up reverse replication from Hetzner to Railway for the 30-day rollback window.

Redis is derived data, so it is not migrated. It starts cold, and every key family must rebuild from Postgres; a test proves this.

## Data backup and retention

- No new learner data is created. Retention is unchanged: general backups 14 days, financial backups 35 days, and the deletion ledger is never expired.
- New:
  - WAL archiving with 7-day PITR.
  - A nightly versioned DR copy of `lesson-assets` in Helsinki.
  - Railway buckets are kept read-only for 35 days after verification.
  - Replication slots are always dropped at detach or rollback.
- The restore drill runs on the Singapore staging stack before prod migration and on the existing schedule afterwards.
- Residency: backups in the EU are a cross-border transfer and must be listed in the privacy notice.

## Analytics

- No new product events. PostHog product and LLM event hourly counts must stay within ±20% of the same hours a week earlier for 24 h after each step that changes traffic (steps 3–5).
- Prometheus alerts cover: disk, WAL-archive lag, backup age, replication lag, RustFS volume usage and DR-copy age.

## Model testing and reliability

- Engine code, provider routing, fallbacks, timeouts and cost limits are unchanged.
- Before steps 4–5, the Engine on Singapore staging must pass:
  - `mise run engine:eval`;
  - the live sample eval at CI thresholds;
  - p95 time-to-first-token no more than 10% above the Railway baseline.

## Risks

- **Ops burden** (Postgres, RustFS, patching): runbooks and alerts. Rehearse steps 1–6 on staging before prod.
- **Railway may refuse or reset `wal_level`, since it is unsupported:** fall back to a planned dump/restore. That breaks zero-downtime, so the owner must re-decide.
- **Railway region far from Singapore makes step 4 slow:** keep step 4 to minutes.
- **Hetzner volume price in Singapore is unpublished:** confirm before ordering.
