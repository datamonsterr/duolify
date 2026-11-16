# E18: Infrastructure cost migration options

- Status: investigation, owner decision pending ([ADR 0014](../adr/0014-infrastructure-cost-migration.md))
- Prices and limits read on **2026-11-16** from the official pages linked in [Sources](#sources). Items marked **(verify)** were not confirmed on an official page and must be checked in the matching ticket before any money is spent.
- Scope: hosting for `services/api` (serve + worker), `engine`, Postgres, Redis, object storage, the static web client, backups and CI deploy. AI provider spend (OpenRouter, Anthropic, ElevenLabs) is the dominant unit cost (`docs/business/monetization.md`) and does not change with hosting.

## 1. What the stack actually needs (from the code, not the ADRs)

These constraints decide which platforms are viable. Each one was confirmed in the repository on 2026-11-16.

| Need | Where | Consequence for hosting |
|---|---|---|
| **S3 bucket versioning** (`PutObject` must return a `VersionId`, `ListObjectVersions`, read by version, delete markers) | `services/api/internal/backup/s3_versions.go` (`PutVersioned` refuses unversioned buckets), `services/api/internal/player/objects.go` (`Restore` removes delete markers) | **Cloudflare R2 cannot hold these buckets today**: R2 marks `PutBucketVersioning`/`GetBucketVersioning` unimplemented and has no `ListObjectVersions`. Using R2 for them needs a code change (key-per-version + R2 bucket locks). Hetzner Object Storage and Backblaze B2 advertise versioning and Object Lock (verify per-action support with the existing conformance tests). |
| Conditional create `PutObject` with `If-None-Match: *` | `player/objects.go`, `backup/s3.go` (append-only deletion ledger) | R2 supports it (412 on conflict). Must be in the conformance test for any new store. |
| **Session-level** `pg_advisory_lock` held across statements on one connection | `lessontest/service.go`, `courses/creation.go`, `narration/service.go` | Breaks behind any **transaction-mode** pooler: Cloudflare Hyperdrive (session locks explicitly unsupported), Neon pooled endpoint, Supabase port 6543. The API and worker must use a **direct or session-mode** Postgres connection. |
| River queue (`riverpgxv5`), LISTEN/NOTIFY with poll fallback, `FOR UPDATE`, transaction-level advisory locks | `internal/worker/worker.go` and services | Same as above. River can run poll-only, but session locks still need a direct connection. Rules out "Postgres behind Hyperdrive from Workers" for the API. |
| Always-on worker (River, streak rollover, nightly encrypted `pg_dump`, restore drills) | `/api worker` | Scale-to-zero for the worker is not possible; an always-on DB connection also keeps scale-to-zero databases (Neon) awake, so their free tiers save nothing. |
| SSE streams (tutor chat, course creation, lesson test) | `chat/turn.go`, `courses/creation.go`, `lessontest/service.go`; server `WriteTimeout` 20 s | Any proxy in front must keep streams open. Cloudflare's proxy returns 524 if the origin sends nothing for 100 s (default Proxy Read Timeout); streams need heartbeats well under that (verify with a spike). |
| Private gRPC (tonic, HTTP/2) API → engine, long LLM calls | `ENGINE_ADDR`, engine has no auth surface (ADR 0002) | Host needs private networking with HTTP/2. Cloudflare Tunnel does not support gRPC on public hostnames; Cloudflare Containers do not document inbound HTTP/2. Same-host or private-network placement avoids both problems. |
| Objects served **through the API** (`GetObject`, no presigned URLs) | `player/objects.go` | Every slide/audio byte is billed as **compute egress** on the current host (Railway $0.05/GB), not as free bucket egress. A CDN in front of cacheable immutable objects removes most of it. |
| Postgres 18 | `docker-compose.yml`, `infra/backup` (`pg_dump 18.6`) | Neon supports 14–18. Supabase docs name 15 and 17 only (PG18 unconfirmed). |

## 2. Load assumptions

Planning numbers, not measurements. Replace them with Railway invoice and load-test data (ticket E18-01).

| | MVP | Growth | Scale |
|---|---|---|---|
| DAU / MAU (DAU/MAU = 1/3) | 100 / 300 | 1k / 3k | 10k / 30k |
| Peak API rps (estimate) | ~5 | ~30 | ~250 |
| Object storage (A14: 0.1 GB per free MAU) | 30 GB | 300 GB | 3 TB |
| Delivery egress (A14: 0.1 GB per free MAU) | 30 GB | 300 GB | 3 TB |
| Postgres data | 10 GB | 50 GB | 200 GB |
| Compute (allocated, all services) | ~1.5 vCPU / 2.5 GB | ~4 vCPU / 6 GB | ~10 vCPU / 16 GB |

The engine is I/O-bound (it waits on LLM providers), so concurrency, not CPU, sizes it. Costs below are **prod only**; dev/staging add 30–60% on usage-billed hosts and roughly nothing on a shared VM.

## 3. Options

### A. Cloudflare-heavy (Workers + Containers + R2 + managed Postgres)

- **Mapping:** web client on Workers static assets; a Worker as edge gateway routing to Cloudflare **Containers** running `api`, `worker` and `engine` images (addressed through Durable Objects); R2 for objects and backups; Postgres on Neon (direct connection from the container with `enableInternet: true`; Hyperdrive is Worker-only and transaction-mode); Redis on Upstash.
- **Code changes:** R2 versioning replacement (key-per-version + bucket locks, rewrite of `backup/s3_versions.go` and player restore), Worker/DO routing layer, container lifecycle for the always-on worker, gRPC API→engine through container ports (undocumented; spike), Postgres connection limits without a pooler, multi-arch images.
- **Cost:** Workers Paid $5 minimum. Containers bill memory $0.0000025/GiB-s (≈ $6.5/GiB-month), CPU $0.000020/vCPU-s while active (≈ $52/vCPU-month at full use, 2.6× Railway's $20), disk $0.00000007/GB-s; 1 TB egress included (NA/EU). Estimates: **~$60 / ~$150–190 / ~$550–650** per month (100 / 1k / 10k DAU) including Neon and Upstash.
- **Latency:** containers cold-start 1–3 s; placement is "nearest pre-initialised location" and may not be near the database, which adds DB round trips to every request. Workers edge latency is excellent for static and cached content only.
- **Scale ceiling:** very high (account limits 15,000 vCPU). DB is the ceiling.
- **Ops / lock-in / exit:** low infra ops, **high lock-in** (DO-addressed containers, Worker routing, R2 bucket locks). Containers GA only since 2026-04-13; legacy Container classes change at 2026-12-31.
- **Risks:** most unknowns (gRPC, SSE through Worker→DO→container, DB latency from roaming placement, CPU price at scale). **Not recommended** for compute; its parts (static assets, CDN, R2) are reused in other options.

### B. Hybrid managed (Cloudflare edge + Fly.io compute + managed Postgres + Upstash)

- **Mapping:** web client on Workers static assets (free, unlimited static requests); Cloudflare DNS/CDN/WAF proxying the API; Fly.io Machines for `api`, `worker`, `engine` over Fly's private 6PN network (same-region traffic free); Postgres on **Supabase Pro** (direct or session-mode pooler, port 5432) or **Neon Launch** (direct endpoint only); Upstash Redis; objects on Hetzner Object Storage (versioned, no code change) or R2 after the versioning change.
- **Code changes:** none in application code if a versioned store is used; config for SSE heartbeats; Fly config + deploy job; multi-region not needed.
- **Cost:** Fly shared-cpu-1x 512 MB $3.69, 1 GB $6.70, performance-1x 2 GB $33; egress $0.02/GB (NA/EU). Supabase Pro $25 incl. $10 compute credit (Micro), Small $15, Large $110, disk $0.125/GB over 8 GB, **PITR add-on $100/month**. Neon Launch $0.106/CU-h (always-on 0.25 CU ≈ $19/month) + $0.35/GB-month. Upstash fixed $10 (250 MB) / $20 (1 GB). Estimates: **~$45 / ~$85 (Supabase) – $145 (Neon) / ~$330–430**.
- **Latency:** no cold starts if Machines stay running; DB in the same region as Fly (verify region pairing) keeps p95 close to today.
- **Scale ceiling:** high (add Machines; DB vertical to Supabase XL / Neon 16 CU).
- **Ops:** low–medium. Three vendors plus Cloudflare. **Lock-in:** low (Docker + plain Postgres).
- **Risks:** managed PITR is the expensive line (Supabase +$100) or Neon storage at scale; Fly has no free allowance for new orgs.

### C. Self-managed VMs (Hetzner Cloud) behind Cloudflare, versioned Hetzner Object Storage

- **Mapping:** Hetzner Cloud VMs run the existing `docker-compose.yml` services (`api`, `worker`, `engine`, `redis`, `postgres`) on a Hetzner private network; Cloudflare DNS/CDN/WAF in front (origin firewalled to Cloudflare IPs, or Cloudflare Tunnel for HTTP only — gRPC stays private); web client on Workers static assets; objects and backups on **Hetzner Object Storage** (S3 API, versioning and Object Lock advertised; €6.49/month base incl. 1 TB storage and 1 TB egress, then €0.0087/TB-hour and €1/TB egress); Postgres PITR by WAL archiving to object storage plus the existing nightly encrypted `pg_dump`.
- **Code changes:** none in application code. New: production compose overlay, deploy job (pull GHCR image by digest, `migrate`, rolling restart), WAL archiving, host hardening, metrics already exist (`infra/observability`).
- **Cost (EUR excl. VAT):** CX33 4 vCPU/8 GB €8.99, CAX21 4 vCPU/8 GB (Arm) €10.99, CX23 2 vCPU/4 GB €5.99; 20 TB traffic included in EU; IPv4 €0.50. Hetzner marked cost-optimized plans "currently unavailable" on 2026-11-16 (capacity), and prices changed in 2026 (verify at order time). Estimates: **~$20 / ~$35 / ~$130–170** (10k: 2 app VMs + 1 DB VM on dedicated vCPU (verify plan price) + ~3 TB object storage).
- **Latency:** no cold starts; API→DB and API→engine on a LAN; EU (Falkenstein/Nuremberg/Helsinki) and US (Ashburn/Hillsboro) locations. Fastest p95 of all options for EU learners.
- **Scale ceiling:** vertical per VM, horizontal app VMs behind Cloudflare; Postgres single primary (add a streaming replica for HA at 10k). Adequate to well beyond 10k DAU for this workload.
- **Ops:** **highest** — OS patching, Postgres upgrades, WAL/PITR, disk alerts, failover are ours. **Lock-in:** lowest (plain Docker + Postgres + S3).
- **Free variant:** Oracle Cloud Always Free Ampere A1 is now **2 OCPU / 12 GB** total (1,500 OCPU-h + 9,000 GB-h per month), 200 GB block storage, 10 TB egress; instances idle below 20% P95 CPU/network/memory for 7 days may be reclaimed and A1 capacity is often unavailable. Fine for **dev/staging**, not for prod.

### D. Stay on Railway, trimmed (baseline)

- **Mapping:** keep `.railway/railway.ts`; put Cloudflare (free DNS/CDN/WAF) in front of the API; move the web client to Workers static assets; send long-cache headers on immutable slide/audio responses so the CDN serves repeats; enable App Sleeping for dev/staging; right-size replicas.
- **Code changes:** cache headers for immutable object routes (authorization rules permitting); nothing else.
- **Cost:** Railway bills **used** resources: CPU $0.00000772/vCPU-s (≈ $20/vCPU-month), RAM $0.00000386/GB-s (≈ $10/GB-month), volumes ≈ $0.15/GB-month, service egress $0.05/GB, buckets $0.015/GB-month with free egress. Hobby $5 (incl. $5 usage, 5 GB volume cap), Pro $20 (incl. $20 usage). Estimates: **~$20 (Pro minimum; usage ≈ $13 fits inside it, but Hobby's 5 GB volume cap is below the 10 GB MVP database) / ~$70 / ~$310–450**; the CDN removes most of the 10k-DAU egress line ($150 untrimmed).
- **Latency/scale/ops:** unchanged; lowest effort; PITR needs Railway HA Postgres conversion. Lock-in: medium (IaC is Railway-specific, images are not).

### E. Cloud Run (considered, not shortlisted)

Cloud Run fits the engine (60-minute request timeout, native gRPC, end-to-end HTTP/2 opt-in, scale to zero, free tier 180,000 vCPU-s + 360,000 GiB-s + 2M requests per month). The always-on worker needs instance-based billing ($0.000018/vCPU-s, $0.000002/GiB-s, 240,000 vCPU-s free): one always-on vCPU is ≈ $42/month after the free tier, plus a managed Postgres. That makes it more expensive than B or C at every load level; the $300/90-day trial credit does not change the steady state. Revisit only if the engine alone needs burst scale.

## 4. Comparison

Monthly prod cost in USD (EUR ≈ USD at 1.15 for estimates). p95 is the API read path from the host region, excluding LLM time.

| Option | 100 DAU | 1k DAU | 10k DAU | p95 read latency | Scale ceiling | Migration effort | Risk |
|---|---|---|---|---|---|---|---|
| A Cloudflare-heavy | ~$60 | ~$150–190 | ~$550–650 | variable; +1–3 s cold start, DB distance | very high | 15–20 days | High |
| B Fly + Supabase/Neon + Upstash | ~$45 | ~$85–145 | ~$330–430 | ≈ today (same-region DB) | high | 8–10 days | Medium |
| **C Hetzner VMs + Cloudflare + Hetzner Object Storage** | **~$20** | **~$35** | **~$130–170** | **best (LAN DB, no cold start)** | medium-high (vertical + app VMs; DB replica) | 7–9 days | Medium (ops) |
| D Railway trimmed (baseline) | ~$20 | ~$70 | ~$310–450 | today | high | 2–3 days | Low |
| E Cloud Run + managed DB | ~$70 | ~$120 | ~$400+ | +cold starts unless min instances | very high | 8–10 days | Medium |

Free credits worth applying for (none assumed in the numbers): Cloudflare for Startups ($10,000 for bootstrapped/<$1M raised, 1 year, needs incorporated for-profit with live site), Google for Startups Start tier (up to $2,000, verify), AWS Activate Founders (up to $5,000), Microsoft for Startups (up to $5,000 after verification), Neon Startup Program (up to $100k, venture-funded only), Koyeb startups (up to $30k). GitHub Student Developer Pack: Azure $100, Heroku $13/month for 24 months; no DigitalOcean offer listed.

## 5. Recommendation

**Phased: D now, then C at a trigger, keeping Railway as warm fallback until C passes its restore drill.**

At 100 DAU Railway trimmed costs about the same as anything else and needs no migration, so moving compute today buys little. The savings appear at 1k–10k DAU, where self-managed Hetzner is 2–3× cheaper than Railway and managed alternatives while giving lower latency (LAN database, no cold starts). R2 is attractive for zero egress, but its missing versioning makes it a code change; Hetzner Object Storage keeps the current S3 code unchanged and includes 1 TB of egress.

| Phase | What moves | Trigger | Effort | Rollback |
|---|---|---|---|---|
| 0 Measure | Export Railway usage per service; k6 load test at 1k-DAU profile; record p95 baselines and SSE timings | now | 1 day | n/a |
| 1 Edge | Cloudflare DNS/CDN/WAF in front of the API (SSE heartbeat < 100 s, verified); web client on Workers static assets; immutable object cache headers; dev/staging App Sleeping | now | 2 days | flip DNS record to DNS-only / back to Railway domain |
| 2 Objects | Conformance-test Hetzner Object Storage (versioning, delete-marker restore, `If-None-Match`, Object Lock); copy buckets; switch `S3_*`/`BACKUP_S3_*` env; keep Railway buckets read-only 35 days | after phase 1 | 2 days | switch env back; Railway buckets untouched |
| 3 Staging on VMs | Compose prod overlay, deploy job, WAL archiving, restore drill on a Hetzner staging VM (or Oracle Always Free) | Railway prod bill > $60/month **or** sustained 500 DAU | 3 days | none needed (staging) |
| 4 Prod cutover | Freeze writes (maintenance mode ≤ 15 min), final `pg_dump`/restore to Hetzner, flip Cloudflare origin, keep Railway stack scaled down but deployable for 30 days | phase 3 restore drill green + owner go | 1–2 days | flip origin back; replay from last dump (RPO = freeze window) |
| 5 Decommission | Remove Railway services after 30 days clean; archive `.railway/` IaC | 30 days after cutover | 0.5 day | redeploy from archived IaC |

CI keeps `ci-ok` and GHCR publishing unchanged. `deploy-dev.yml` gains a second target (SSH or a pull-based agent on the VM deploying an image digest; verify mechanism in ticket) behind the same `ci-ok` gate; Railway plan/apply jobs stay until phase 5.

## 6. Open questions for the owner

1. Where are learners? EU-only Hetzner locations favour Europe; US learners would use Ashburn/Hillsboro (fewer plan types) or option B.
2. Is the operational burden of self-managed Postgres (patching, WAL/PITR, upgrades) acceptable, or should phase 4 use Supabase Pro (+$25–135/month, +$100 for PITR) with Hetzner compute?
3. Is the company incorporated, with a live site, so it can apply for Cloudflare for Startups ($10k) or other credits?
4. Accept a ≤ 15-minute write freeze for the production cutover?
5. Should R2 replace Hetzner Object Storage later (zero egress) at the cost of rewriting the versioning-based backup and restore code?

## Sources

All read 2026-11-16.

- Cloudflare Workers limits / pricing: https://developers.cloudflare.com/workers/platform/limits/ , https://developers.cloudflare.com/workers/platform/pricing/ , static assets https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/
- Cloudflare Containers: pricing https://developers.cloudflare.com/containers/pricing/ , limits https://developers.cloudflare.com/containers/platform-details/limits/ , FAQ https://developers.cloudflare.com/containers/faq/ , outbound https://developers.cloudflare.com/containers/platform-details/outbound-traffic/ , GA https://developers.cloudflare.com/changelog/post/2026-04-13-containers-sandbox-ga/
- Cloudflare R2: pricing https://developers.cloudflare.com/r2/pricing/ , S3 compatibility https://developers.cloudflare.com/r2/api/s3/api/ , bucket locks https://developers.cloudflare.com/r2/buckets/bucket-locks/ , presigned URLs https://developers.cloudflare.com/r2/api/s3/presigned-urls/
- Cloudflare Hyperdrive: https://developers.cloudflare.com/hyperdrive/concepts/how-hyperdrive-works/ , https://developers.cloudflare.com/hyperdrive/platform/limits/ , https://developers.cloudflare.com/hyperdrive/platform/pricing/
- Cloudflare Queues / Durable Objects / Workflows: https://developers.cloudflare.com/queues/platform/pricing/ , https://developers.cloudflare.com/durable-objects/platform/pricing/ , https://developers.cloudflare.com/workflows/reference/limits/
- Cloudflare Tunnel FAQ (gRPC): https://developers.cloudflare.com/cloudflare-one/faq/cloudflare-tunnels-faq/ ; error 524 (100 s Proxy Read Timeout): https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-5xx-errors/error-524/ ; CDN large-file terms: https://www.cloudflare.com/service-specific-terms-application-services/ ; Startups: https://www.cloudflare.com/forstartups/
- Railway: https://railway.com/pricing , https://docs.railway.com/reference/app-sleeping , https://docs.railway.com/databases/postgresql
- Neon: https://neon.com/pricing , https://neon.com/docs/connect/connection-pooling , https://neon.com/docs/postgresql/postgres-version-policy
- Supabase: https://supabase.com/pricing , https://supabase.com/docs/guides/database/connecting-to-postgres
- Upstash: https://upstash.com/pricing/redis ; Redis Cloud: https://redis.io/pricing/
- Fly.io: https://docs.fly.io/about/pricing , https://docs.fly.io/networking/private-networking
- Koyeb: https://www.koyeb.com/pricing , https://www.koyeb.com/docs/run-and-scale/scale-to-zero
- Google Cloud Run: https://cloud.google.com/run/pricing , https://docs.cloud.google.com/run/docs/configuring/request-timeout , https://docs.cloud.google.com/run/docs/configuring/http2 , free tier https://docs.cloud.google.com/free/docs/free-cloud-features
- Oracle Always Free: https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm
- Hetzner Cloud: https://www.hetzner.com/cloud/cost-optimized/ , https://www.hetzner.com/cloud/regular-performance/ ; Object Storage: https://www.hetzner.com/storage/object-storage/pricing/ , https://docs.hetzner.com/storage/object-storage/overview , https://docs.hetzner.com/storage/object-storage/supported-actions
- Backblaze B2: https://www.backblaze.com/cloud-storage/pricing , https://www.backblaze.com/docs/cloud-storage-s3-compatible-api
- Credits: https://aws.amazon.com/startups/credits , https://www.microsoft.com/en-us/startups , https://cloud.google.com/startup , https://education.github.com/pack
- River poll-only mode: https://github.com/riverqueue/river/issues/302
