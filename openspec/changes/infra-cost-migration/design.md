# Design: infrastructure cost migration

## Target topology (after phase 4)

```text
learner ─▶ Cloudflare (DNS, CDN, WAF, Workers static assets for web client)
              │  proxied HTTPS, origin firewalled to Cloudflare IP ranges
              ▼
        Hetzner private network
        ├─ app VM(s): api serve (HTTP + SSE), worker (River), engine (gRPC, private only), redis
        └─ db VM:     postgres 18 (+ streaming replica at 10k DAU)
              │ WAL archive + nightly age-encrypted pg_dump
              ▼
        Hetzner Object Storage (versioned buckets: assets, backups; Object Lock on ledger prefix)
```

## Key decisions

- **Direct Postgres connections.** Session-level `pg_advisory_lock` (lessontest, course creation, narration) and River require session semantics. No transaction-mode pooler (Hyperdrive, PgBouncer transaction mode) sits in front of the API or worker.
- **Versioned object storage.** `backup.PutVersioned` refuses unversioned buckets, and player restore removes delete markers. Hetzner Object Storage advertises versioning and Object Lock. The conformance suite proves every call the code makes before any data moves. R2 is deferred because it lacks versioning.
- **Engine stays private.** gRPC runs over the Hetzner private network. Cloudflare Tunnel public hostnames do not carry gRPC, so the engine is never behind Cloudflare.
- **SSE through Cloudflare.** Cloudflare returns 524 after 100 s without origin data. The API sends a comment heartbeat at most every 25 s on every SSE route. A spike verifies streams of at least 10 minutes through the proxy.
- **Caching.** Only content-addressed or immutable object responses that need no per-learner authorization get `Cache-Control: public, max-age=31536000, immutable`. Authorized manifests stay `private, no-store`.
- **Rollback.** Railway stays deployable with the last known-good image digests for 30 days. Rollback is a Cloudflare origin flip plus a restore of the latest dump. Its RPO is the data written since the last dump; the cutover freeze makes that zero during the cutover window.

## Data backup and retention

- No new learner data is created. Existing retention is unchanged: general backups 14 days, financial backups 35 days, and the append-only deletion ledger is never expired.
- New: continuous WAL archiving gives PITR within 7 days, in addition to the nightly encrypted `pg_dump`. The restore drill (`infra/backup/restore-drill.sh`) runs against the VM before cutover and on the existing schedule afterwards.
- Railway buckets are kept read-only for 35 days after the object migration, then deleted. The copy is checked by object count, total bytes, per-key checksum sample and version count.

## Analytics

- No new product events. Existing PostHog product and LLM events must continue. Cutover verification compares hourly event counts for 24 h before and after the switch (tolerance ±20% adjusted for time of day).
- Operational metrics remain in Prometheus (`infra/observability`). Disk, WAL-archive lag and backup-age alerts are added.

## Model testing and reliability

- The engine binary, provider routing, fallbacks, timeouts and cost limits are unchanged.
- Before cutover, `mise run engine:eval` (mock) and the live sample eval run against the engine on the new host, and must meet the same thresholds as CI.
- The engine p95 time-to-first-token is compared with the Railway baseline (budget: no more than +10%).

## Risks

- **Ops burden** of self-managed Postgres. Mitigations: runbooks, alerts, and a documented fallback to Supabase Pro.
- **Hetzner capacity:** cost-optimized plans were listed as unavailable on 2026-11-16. Regular-performance plans are the fallback, with a higher cost.
- **Cloudflare free-plan CDN terms** for a disproportionate share of audio. Watch the audio share. If notified, move audio delivery to R2 (paid service) after the versioning rewrite.
