# E18: Infrastructure cost migration options

- Status: **decided 2026-11-17, revised the same day: Option C in Hetzner Helsinki, Cloudflare in front, zero-downtime cutover with a ≤15-minute read-only fallback** ([ADR 0014](../adr/0014-infrastructure-cost-migration.md), Accepted; backups on Hetzner Object Storage FSN/NBG, decided 2026-11-17). §10 is the current plan. §1–§4 are the 2026-11-16 comparison and §5–§9 the superseded Singapore plan, both kept for context.
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

## 5. Owner decisions (2026-11-17, Singapore — superseded by §10)

Recorded in [ADR 0014](../adr/0014-infrastructure-cost-migration.md) (Accepted). The phased "D first" recommendation of 2026-11-16 is superseded.

1. **Option C, straight away.** Compute, Postgres, Redis and objects all move to Hetzner Cloud VMs. They run the existing Docker stack behind Cloudflare. There is no Railway-trimming detour.
2. **Region: Southeast Asia.** Most learners are there, so everything runs in Hetzner **Singapore (`sin`, network zone `ap-southeast`)**.
3. **Postgres is self-managed on the VM.** It reuses `infra/backup` (encrypted `pg_dump`, restore drills) and adds WAL archiving.
4. **Zero-downtime cutover.** There is no maintenance window or write freeze: logical replication runs from Railway to Hetzner, followed by a seconds-long, request-holding switchover (section 7).
5. **No startup credits for now.**

## 6. Former design: Option C in Singapore (superseded by §10)

Prices below were read on 2026-11-17 from Hetzner's public price service. It backs the plan tables on hetzner.com (`website-price-api.hetzner.com/api/v1/products/<id>`) and is cross-checked against https://www.hetzner.com/cloud-singapore/. All prices are EUR excl. VAT. The figures include a primary IPv4 at €0.50 (matching the page's "from" prices), and USD is about EUR × 1.17 (the price service's own USD figures).

### Singapore availability (official, 2026-11-17)

| Item | Singapore (`sin`) | Source |
|---|---|---|
| Cloud Shared AMD (CPX), Cloud Dedicated AMD (CCX) | ✓. Cost-optimized (CX/CAX) is **not** offered | https://docs.hetzner.com/cloud/general/locations/ |
| Volumes, Networks, Firewalls, Load Balancers, Backups, Snapshots, Floating IPs | ✓. Networks and LBs must be in the same network zone (`ap-southeast`) | same |
| **Object Storage** | **✗. Only Falkenstein, Nuremberg and Helsinki** | same; https://docs.hetzner.com/storage/object-storage/overview |
| Traffic | 0.5–5 TB outbound included depending on plan, then **$8.49/TB** (EU: €1/TB). Internal and private-network traffic is free | https://www.hetzner.com/cloud-singapore/ ; https://docs.hetzner.com/cloud/billing/faq/ |
| Volume price | not published on a fetchable official page (**verify** in the console before ordering) | https://www.hetzner.com/cloud/block-storage/ |

| Plan | vCPU / RAM / disk | SIN €/month | HEL €/month (reference) |
|---|---|---|---|
| CPX12 | 1 shared / 2 GB / 40 GB | 15.99 | 9.99 |
| CPX22 | 2 shared / 4 GB / 80 GB | 26.99 | 16.99 |
| CPX32 | 4 shared / 8 GB / 160 GB | 49.49 | 30.49 |
| CPX42 | 8 shared / 16 GB / 320 GB | 93.99 | 59.49 |
| CCX13 | 2 dedicated / 8 GB / 80 GB | 54.49 | 36.99 |
| CCX23 | 4 dedicated / 16 GB / 160 GB | 108.99 | 73.49 |
| CCX33 | 8 dedicated / 32 GB / 240 GB | 174.99 | 117.99 |

Singapore costs about 1.5–1.6× Helsinki. **This erodes the savings estimated on 2026-11-16 (EU prices).**

### Object storage without Hetzner Object Storage in Singapore

Railway's buckets are in `sin` today (`.railway/railway.ts`), so assets served from Helsinki would add an intercontinental round trip to every uncached slide or audio read. Lessons are mostly per-learner, so the CDN hit rate is low on first view. Decision:

- **`lesson-assets`: self-hosted RustFS** on the Singapore app VM, with data on a Hetzner Volume. It is the same `rustfs/rustfs` image `docker-compose.yml` uses, and the repository's backup and player integration tests already exercise versioning, delete-marker restore and `If-None-Match` against it. The conformance suite (E18) re-proves this on the target.
- **`backups` (pg_dump, WAL archive, deletion ledger) and a nightly versioned DR copy of `lesson-assets`: Hetzner Object Storage, Helsinki.** It is off-site from Singapore, versioned with Object Lock advertised, and costs €6.49/month including 1 TB storage and 1 TB egress (then €0.0087/TB-hour and €1/TB). The SG→HEL round trip only affects backup writes and account-deletion ledger writes, which are not latency-critical.
- **Considered and rejected for now:**
  - **Cloudflare R2 with an APAC location hint:** no S3 versioning, so it needs a code rewrite.
  - **Hetzner Object Storage in Helsinki as the primary store:** adds a long round trip on cache misses.
- **Residency note:** backups stored in the EU are a cross-border transfer from Singapore. Record this in the privacy notice (owner/legal).

### Topology

```text
learner (SEA) ─▶ Cloudflare (DNS, CDN, WAF; web client on Workers static assets; SIN edge)
                   │ proxied HTTPS; origin firewall allows Cloudflare ranges only
                   ▼
   Hetzner sin, private network (ap-southeast)
   ├─ app VM:  api serve ×2 (HTTP + SSE), worker (River), engine (gRPC, private), redis, rustfs (+ volume)
   └─ db VM:   postgres 18 (local NVMe), WAL archive ─┐
                                                     ▼
   Hetzner Object Storage hel1: backups (versioned, ledger locked), lesson-assets DR copy
```

### Cost (prod, per month, SIN)

| Load | Layout | Hetzner compute | Object Storage HEL | Volume (assets) | Est. total | Railway est. |
|---|---|---|---|---|---|---|
| 100 DAU | 1× CPX22 (everything) | €26.99 | €6.49 | 30 GB (verify price) | **~€33.5 ≈ $39** + volume | ~$20 |
| 1k DAU | CPX32 app + CCX13 db | €103.98 | €6.49 | 300 GB (verify) | **~€110 ≈ $129** + volume | ~$70 |
| 1k DAU, lean | 1× CPX32 (everything) | €49.49 | €6.49 | 300 GB (verify) | **~€56 ≈ $66** + volume | ~$70 |
| 10k DAU | CPX42 app + CCX23 db (+ CPX22 engine/worker) | €229.97 | ~€19 (3 TB DR) | 3 TB (verify) | **~€249 ≈ $291** + volume | ~$310–450 |

Traffic: Cloudflare caches immutable objects, so origin egress stays inside the 0.5–5 TB included allowance at 1k DAU. Above it, Singapore egress is $8.49/TB, against $50/TB on Railway.

**Honest read:** in Singapore, Option C costs **more than Railway at 100 DAU** and about the same at 1k DAU (lean layout). It saves roughly 5–35% at 10k DAU before volume costs. That is far less than the EU estimate. The other gains are a fixed, predictable bill, a LAN database, and no per-GB egress shock ($8.49/TB against $50/TB). Railway's rates were read on 2026-11-16 and are not region-dependent on its pricing page.

## 7. Zero-downtime cutover design (step 3 revised in §10.9)

Facts (read 2026-11-17):

- **Railway's Postgres image:** the template deploys `ghcr.io/railwayapp-templates/postgres-ssl:18`. Railway documents no `wal_level=logical`, REPLICATION role or logical-replication guide. Setting config through `ALTER SYSTEM` is allowed but ends Railway database support, and **`wal_level` changes need a Postgres restart**.
- **What PostgreSQL 18 logical replication does not copy:** DDL, **sequences**, large objects or NOTIFY. Every table needs a primary key or replica identity.
- **River:** elects one leader per database through the unlogged `river_leader` table (5 s TTL). Its `Stop` performs a soft stop with `SoftStopTimeout`.
- **Advisory locks are session-local and never replicated.**

| Step | Action | User impact |
|---|---|---|
| 0 Preflight | Owner checks `SHOW wal_level`, role attributes, Railway service region. Agent query lists tables without PK/replica identity and any large objects. **If `wal_level` ≠ `logical`:** schedule one Railway Postgres restart in the lowest-traffic minute (seconds of reconnects; pgx pools retry). This is the only unavoidable blip. | ≤ seconds, once |
| 1 Objects | Copy `lesson-assets` and `backups` (all versions) Railway → RustFS SIN / HEL with `infra:s3-copy-verify`. Deploy **read-fallback**: reads miss the new store, then fall back to the Railway bucket. Switch writes to the new stores (rolling deploy), then run an incremental copy and verify. Remove the fallback after verification. | none |
| 2 Replicate | `pg_dump --schema-only` → Hetzner. Publication `FOR ALL TABLES` on Railway (TLS via TCP proxy), subscription on Hetzner with `copy_data`. Monitor lag (`pg_stat_subscription`, `pg_stat_replication`). **Migrations are frozen** until step 5 (DDL is not replicated). | none |
| 3 Shift compute | Deploy the Hetzner app stack **pointing at Railway Postgres** (same DB → no split brain; River elects one leader across both stacks; advisory locks shared). Shift traffic with the Cloudflare origin, then scale Railway compute to zero. Requires Railway to be in Singapore or nearby, otherwise this step adds DB latency and should be kept short (**verify region**). | none (rolling) |
| 4 DB switchover | Hetzner API enters **switchover gate** (new code): new DB work is held, not rejected, up to 15 s; in-flight transactions and session-lock holders finish (bounded wait); River `Stop` (soft). Railway DB: `ALTER DATABASE … SET default_transaction_read_only = on` + terminate remaining sessions. Wait until subscriber LSN = publisher LSN. `setval` every sequence from Railway values + safety margin. Swap the pgx pool to Hetzner DB, start River, release the gate. | requests pause ≤ 15 s (target < 5 s); no errors |
| 5 Detach | Drop subscription; **drop the replication slot on Railway** (an orphaned slot fills the disk); unfreeze migrations. | none |
| Rollback before 5 | Release gate back to the Railway pool (Railway is still the publisher; clear `read_only`). | ≤ 15 s pause |
| Rollback after 5 | Reverse logical replication Hetzner → Railway is set up during the 30-day fallback window (publication on Hetzner, subscription on Railway), so the same gate flips back. | ≤ 15 s pause |

Redis is not migrated: it is derived data (ADR 0003). The Hetzner Redis starts cold, and rate-limit windows reset once. A ticket checks that every Redis key family rebuilds from Postgres.

## 8. Phased plan (Singapore; revised in §10.10)

| Phase | Content | Owner / agent | Effort |
|---|---|---|---|
| 0 Preflight | Owner: Hetzner + Cloudflare accounts, Railway `wal_level`/region/role facts. Agent: PK/large-object audit query, k6 baseline. | both | 1 day |
| 1 Build | Agent: compose prod overlay (RustFS, Postgres tuning, WAL archiving to HEL), SSE heartbeats + cache headers, S3 conformance, copy/verify + read-fallback, switchover gate, deploy job. | agent | 5–7 days |
| 2 Staging in SIN | Owner provisions staging VMs and secrets. Agent runs the restore drill, `engine:eval` + live sample eval, load test, and a rehearsal of steps 1–5 against a Railway staging DB. | both | 2 days |
| 3 Edge | Cloudflare in front of Railway prod and web client on Workers static assets (no origin change yet). | owner | 0.5 day |
| 4 Prod migration | Steps 0–5 of section 7. | owner executes runbook | 1 day + 30-day watch |
| 5 Decommission | Remove Railway after 30 clean days. Update ADR 0003/0008 and archive `.railway/`. | agent + owner | 0.5 day |

CI: `ci-ok` and GHCR publishing are unchanged. The deploy job (Namespace runner) deploys image digests to the VMs after `ci-ok`. The mechanism (SSH with a restricted deploy key, or a pull-based agent on the VM) is decided in its ticket. Railway plan/apply jobs remain until phase 5.

## 9. Remaining open items (Singapore; replaced by §10.11)

1. Railway Postgres facts (`wal_level`, REPLICATION attribute, service region). These decide whether step 0 needs a restart, and whether step 3 adds latency.
2. Hetzner Volume price in Singapore (console).
3. Legal: backups in the EU (Helsinki) for Singapore-hosted learner data.
4. Revisit R2 (zero egress, APAC hint) only if the versioning code is rewritten.

## 10. Revision 2026-11-17: Helsinki, SE Asia latency and Contabo

All prices and limits in this section were read on **2026-11-17**, are monthly and exclude VAT. Hetzner prices come from its public price service (`website-price-api.hetzner.com/api/v1/products/<id>`, which backs the hetzner.com plan pages) and include a primary IPv4 at €0.50 / $0.60. Hetzner USD figures are Hetzner's own. Contabo prices come from the price data embedded in contabo.com product and pricing pages, monthly term, no discount. Sources are listed under [Sources](#sources).

### 10.1 Owner decisions (2026-11-17, supersede §5 items 2–4)

1. **Region: Hetzner Helsinki (`hel1`, network zone `eu-central`)** instead of Singapore. It is the cheapest Hetzner location. The owner accepts the extra latency for Southeast Asian learners, with Cloudflare edge-caching static and object reads.
2. **Cutover.** Zero-downtime logical replication stays plan A. If Railway cannot run `wal_level=logical`, the accepted fallback is a planned read-only window of at most 15 minutes using dump/restore (§10.9).
3. **Contabo evaluated** (§10.6–§10.8).
4. **Unchanged:** no startup credits, and Postgres is self-managed on the VM.

### 10.2 Hetzner Helsinki prices

| Plan | vCPU / RAM / disk | HEL1 €/month | HEL1 $/month | SIN1 €/month (for reference) |
|---|---|---|---|---|
| CX33 (cost-optimized, Intel/AMD shared) | 4 / 8 GB / 80 GB | 8.99 | 10.59 | not offered |
| CPX12 | 1 / 2 GB / 40 GB | 9.99 | 11.59 | 15.99 |
| CPX22 | 2 / 4 GB / 80 GB | 16.99 | 20.09 | 26.99 |
| CPX32 | 4 / 8 GB / 160 GB | 30.49 | 36.09 | 49.49 |
| CPX42 | 8 / 16 GB / 320 GB | 59.49 | 70.09 | 93.99 |
| CCX13 (dedicated) | 2 / 8 GB / 80 GB | 36.99 | 43.09 | 54.49 |
| CCX23 (dedicated) | 4 / 16 GB / 160 GB | 73.49 | 86.59 | 108.99 |

- **Cost-optimized availability.** The cost-optimized page showed CX/CAX plans as "currently unavailable" on 2026-11-17. Plans below therefore use CPX, and CX33 is the cheaper option if it is in stock at order time.
- **Traffic.** EU servers include at least 20 TB outbound. Each extra TB costs €1.00. Singapore now costs €7.40 / $8.30 per TB in the price service, against $8.49 read on 2026-11-16.
- **Volumes:** €0.044 per GB-month, from 10 GB up to 10 TB, triple-replicated.
- **Snapshots:** €0.0143 per GB-month.
- **Object Storage** (Falkenstein, Nuremberg, Helsinki):
  - **Base price:** €6.49 / $7.99 per account per month, across all locations and buckets. It includes 1 TB of storage and 1 TB of egress.
  - **Above the included quota:** €6.26 / $8.86 per TB-month of storage and €1.00 / $1.20 per TB of egress.
  - **Free:** ingress, S3 operations, and internal traffic within `eu-central`. That covers HEL VM ↔ HEL bucket and HEL ↔ FSN/NBG buckets.
  - **Limits:** 750 requests/s per bucket and per source IP, 10 Gbit/s per bucket, 100 TB and 50 million objects per bucket.
  - **Versioning and Object Lock** (retention and legal hold) are documented. The supported-actions page lists only the unsupported calls.

### 10.3 Lesson assets: Object Storage in Helsinki, not RustFS on a Volume

| `lesson-assets` size | Hetzner Object Storage HEL1 (marginal) | RustFS on a HEL Volume |
|---|---|---|
| 30 GB (100 DAU) | €0 (inside the 1 TB quota; the base is paid for backups anyway) | €1.32 |
| 300 GB (1k DAU) | €0 | €13.20 |
| 3 TB (10k DAU) | ~€12.5 (2 TB over quota) | €132 |

**Decision:** `lesson-assets` goes on Hetzner Object Storage in Helsinki.
- It is versioned. The API reads it over free `eu-central` traffic, and Cloudflare caches immutable objects.
- `infra:s3-conformance` must pass first: versioned put, list versions, get by version, delete-marker restore, `If-None-Match: *` and retention lock.
- If it fails, RustFS on a Volume (the same image as `docker-compose.yml`) is the fallback.

### 10.4 Cost at 100 / 1k / 10k DAU (prod, Helsinki)

| Load | Layout | Compute €/$ | Object Storage €/$ | Total €/$ | Singapore plan (§6) | Railway |
|---|---|---|---|---|---|---|
| 100 DAU | 1× CPX22 (everything) | 16.99 / 20.09 | 6.49 / 7.99 (assets 30 GB + backups) | **23.48 / ~$28** (CX33 if in stock: ~$19) | ~$39 + volume | ~$20 |
| 1k DAU, lean | 1× CPX32 (everything) | 30.49 / 36.09 | 6.49 / 7.99 (0.3 TB assets + 0.3 TB DR + 0.1 TB backups, inside 1 TB) | **36.98 / ~$44** | ~$66 + volume | ~$70 |
| 1k DAU | CPX32 app + CCX13 db | 67.48 / 79.18 | 6.49 / 7.99 | **73.97 / ~$87** | ~$129 + volume | ~$70 |
| 10k DAU | CPX42 app + CCX23 db + CPX22 engine/worker | 149.97 / 176.77 | ~40.9 / ~56.7 (3 TB assets + 3 TB DR + 0.5 TB backups) | **~190.9 / ~$234** | ~$291 + volume | ~$310–450 |

- **Origin traffic.** Even 3 TB a month of uncached delivery fits inside the 20 TB included per VM.
- **Object Storage egress.** Delivery and DR copies stay on internal `eu-central` traffic and are not billed.
- **Against Railway:**
  - Helsinki costs about $8 more at 100 DAU.
  - The lean layout is about $26 cheaper at 1k DAU.
  - At 10k DAU it is 25–48% cheaper.
- **Against the Singapore plan:** about 20–33% cheaper at every load level, before Singapore volume costs.

### 10.5 Latency impact for Southeast Asian learners

**Measured RTT.** Source: WonderNetwork global ping statistics (`wondernetwork.com/pings/<A>/<B>`). Each value is the median of the hourly 30-ping averages, n = 336, covering 2026-11-02 to 2026-11-16 UTC. These are WonderNetwork's own servers, not Hetzner's, so the figures approximate Hetzner paths.

| City | from Helsinki (ms) | from Singapore (ms) | Extra with a Helsinki origin (ms) |
|---|---|---|---|
| Singapore | 176.6 | ~1 (local) | ~+175 |
| Kuala Lumpur | 181.6 | 8.6 | +173 |
| Jakarta | 183.5 | 12.9 | +171 |
| Bangkok | 201.6 | 23.1 | +178 |
| Manila | 209.5 | 29.0 | +180 |
| Hanoi | 227.8 | 53.1 | +175 |
| Ho Chi Minh City | 237.1 | 28.4 | +209 |

Frankfurt and Nuremberg are about 155–158 ms from Singapore, about 20 ms closer than Helsinki.

**What the learner sees.** Cloudflare has edges in Singapore, Hanoi, Ho Chi Minh City, Da Nang, Jakarta, Manila, Bangkok and Kuala Lumpur, and keeps warm keep-alive connections to the origin.

| Path | Effect of the Helsinki origin |
|---|---|
| Cached immutable object (slide, audio, web client) | None. Served from the Southeast Asian edge. |
| Object cache miss | +~175–210 ms once per object per tier. With Tiered Cache (Free plan) the upper tier sits near the origin, so a miss crosses to Europe once and later edges hit the tier. |
| Uncached API call over a warm edge→origin connection | +~175 ms (≈ +210 ms from Ho Chi Minh City) per sequential call. A screen that chains N calls pays N × ~175 ms. |
| Uncached API call over a cold edge→origin connection | Add 2–3 round trips for TCP+TLS: +~350–530 ms. This is rare with Connection Reuse. |
| SSE (Tutor chat, course creation, lesson test) | Time to first byte +~175 ms, once per stream. Token streaming throughput is unchanged. Heartbeats must arrive within Cloudflare's **125 s** Proxy Read Timeout (Cloudflare docs, modified 2026-11-13, correcting the 100 s figure in §1). The 25 s heartbeat stays. |

Argo Smart Routing ("Smart Shield + Argo", from $5/month) claims a 30% average gain. No measurement exists for Southeast Asia to Helsinki, and the per-GB rate was not confirmed on an official page. Decide it after staging measurements.

Mitigations, all without new infrastructure:
- immutable cache headers (E18-03);
- Tiered Cache on;
- collapse chained API calls on hot screens where the k6/RUM data shows N > 2.

**Acceptance change:**
- API p95 and Engine TTFT are gated **server-side** against the Railway baseline.
- Client-observed p95 from a Southeast Asian vantage point is recorded in `docs/infra/baseline.md` before and after the move, not gated.

### 10.6 Contabo facts (official pages, read 2026-11-17)

**Core Cloud VPS** (shared vCPU; EU = Germany, no surcharge):

| Plan | vCPU / RAM / disk | Port | € | $ | Singapore surcharge |
|---|---|---|---|---|---|
| Cloud VPS 4 | 4 / 8 GB / 100 GB SSD | 200 Mbit/s | 5.50 | 6.60 | +€2.50 / $3.10, **out of stock** |
| Cloud VPS 6 | 6 / 12 GB / 100 GB NVMe | 300 Mbit/s | 7.50 | 9.00 | +€3.95 / $4.90, **out of stock** |
| Cloud VPS 8 | 8 / 24 GB / 150 GB NVMe | 600 Mbit/s | 14.00 | 16.80 | +€7.85 / $9.75, **out of stock** |
| Cloud VPS 12 | 12 / 48 GB / 200 GB NVMe | 800 Mbit/s | 25.00 | 30.00 | +€14.05 / $17.40, **out of stock** |
| Cloud VPS 16 | 16 / 64 GB / 250 GB NVMe | 1 Gbit/s | 37.00 | 44.50 | +€20.75 / $25.75, **out of stock** |
| Cloud VPS Plus 4 (EPYC) | 4 / 8 GB / 150 GB NVMe | 500 Mbit/s | 13.50 | 16.25 | +€7.55 / $9.35 |

**VDS** ("Max Performance", dedicated AMD EPYC 7282 cores):

| Plan | Cores / RAM / disk | Port | € | $ | Singapore surcharge |
|---|---|---|---|---|---|
| VDS S | 6 / 24 GB / 180 GB NVMe | 250 Mbit/s | 54 | 65 | +€27.50 / $34.10 |
| VDS M | 8 / 32 GB / 240 GB NVMe | 500 Mbit/s | 68 | 81.50 | +€34.80 / $43.15 |
| VDS L | 12 / 48 GB / 360 GB NVMe | 750 Mbit/s | 96 | 115 | +€48.80 / $60.50 |

**Terms:** 12-month terms save 15% and 24-month terms 20%. The site's headline prices are the 24-month rate. There are no setup fees, except Cloud VPS 4 on a 1-month term (€2).

**Regions:** EU (one "European Union" choice served from data centres in Germany, with no city choice), UK, US East, US Central, US West, Singapore, Japan, Australia and India. **No Finland.**

**Object Storage** (existing customers, ordered in the control panel):
- **Price per 250 GB:** €2.49 / $2.99 in the EU and US, €2.99 / $3.39 in Singapore.
- **Egress:** "free and unlimited".
- **Endpoints:** `eu2`, `sin1` and `usc1`.
- **Durability:** triple replication on Ceph.
- **S3 compatibility:** the product page says "fully S3-compatible", but the help centre says "not fully compatible".
- **Versioning:** documented.
- **Object Lock:** documented with GOVERNANCE mode. COMPLIANCE mode is unconfirmed.
- **Limits:** **10 MB/s (80 Mbit/s) bandwidth**, 250 S3 requests/s, 3 million objects per customer (can be raised), 100 buckets.

**Network:** inbound is unmetered. Outbound is "unlimited" under fair use, and Contabo "reserves the right to throttle" heavy users. Terms clause 11(2) says bandwidth is not guaranteed.

**SLA:** 99.9% annual average for physical connectivity (Terms clause 11(1)), excluding maintenance and force majeure. No service credits are stated.

**Snapshots and backups:**
- 1–3 snapshots are included, kept 30 days.
- Auto Backup keeps 10 daily versions, at €1.65–10.00 per month on VPS. It is **not available on VDS**, which offers paid FTP backup space instead.

**Extras:**
- Private networking costs €2.29 / $2.99 per VPS and €5.79 / $7.49 per VDS.
- DDoS protection is free.

**Support:** average response 2 h, English and German, based in Germany. The page itself says waits are currently longer than usual. No contractual response time.

**Benchmarks** (third-party):

| Source | Hetzner | Contabo |
|---|---|---|
| Experte (2026-01-07, YABS, ~$30/month plans) | CX23: Geekbench 6 1442 single / 7945 multi; 57.7k 4k IOPS; 5020 Mbit/s iperf | 482 / 1978; 21.4k IOPS; 292 Mbit/s |
| VPSBenchmarks (2026) | Consistency score 71; provisioning 32 s | Consistency score 56; provisioning 60 s; Cloud VPS 18 stability grade 0.8/20 |

The sources disagree on raw CPU: VPSBenchmarks' sysbench shows Contabo Plus 4 ahead of a CX23. Both show Hetzner more consistent.

**Outages:** a third-party mirror of contabo-status.com lists a ~10.5 h US West disruption (2026-04-27) and a ~44 h "Hub Europe" connectivity incident (2026-09-08). These were not confirmed on Contabo's own status page, and Hetzner's history was not collected.

### 10.7 Contabo compared with Hetzner

**Backups/DR only.** The primary stack stays in Hetzner Helsinki. Size: dumps and WAL of 0.1–0.5 TB, plus an optional DR copy of assets of 0.03–3 TB.

| Target | Monthly cost (backups ≤ 0.5 TB / + 3 TB assets DR) | Restore speed | Independence | Residency |
|---|---|---|---|---|
| **Hetzner Object Storage FSN1/NBG1** | ~€0 inside the shared 1 TB quota / ~€19 | up to 10 Gbit/s per bucket; free `eu-central` traffic | off-site (different city); same provider and account | EU (Germany) |
| Hetzner Object Storage HEL1 (same site as primary) | same | same | **not off-site** | EU (Finland) |
| Contabo Object Storage EU | $2.99–5.98 / ~$42 | **80 Mbit/s cap**: 200 GB ≈ 5.6 h, 3 TB ≈ 3.5 days | different provider | EU (Germany) |
| Contabo Object Storage Singapore | $3.39–6.78 / ~$48 | 80 Mbit/s cap plus a ~175 ms path | different provider | **adds Singapore** |

**Whole Option C host.** USD, monthly term.

| Load | Hetzner Helsinki | Contabo EU (Germany) | Contabo Singapore |
|---|---|---|---|
| 100 DAU | ~$28 (CPX22 + OS) | ~$15 (VPS 6 + 2 × 250 GB OS) | ~$32 (Plus 4, since Core VPS is out of stock; + OS SG + OS EU backups) |
| 1k DAU | ~$44 lean / ~$87 split | ~$42 (VPS 12 + 4 × 250 GB OS) | ~$38 lean (Plus 4 + OS) / ~$148 split (+ VDS S db + private network) |
| 10k DAU | ~$234 | ~$214 (VDS M db + VPS 16 app + private network + 6.5 TB OS) | ~$321 (VDS M + VDS S + private network + OS SG + OS EU backups) |

| | Hetzner Helsinki | Contabo EU | Contabo Singapore |
|---|---|---|---|
| SEA latency (uncached) | +~175 ms | +~155 ms (Germany) | best (~10–50 ms) |
| CPU and disk consistency | higher (Experte, VPSBenchmarks) | lower; fair-use throttling | same as EU |
| Asset store limits | 750 req/s, 10 Gbit/s per bucket | **80 Mbit/s, 250 req/s per account** | same |
| Billing | hourly with a monthly cap; no commitment | monthly, cheaper on 12–24-month terms | same; Core VPS out of stock |
| SLA / support | no SLA figure collected; ticket support | 99.9% connectivity, no credits; 2 h average response, slower now | same |
| Ops tooling | API, Terraform provider, firewalls, private networks | API/CLI, paid private networking | same |

### 10.8 Recommendation

1. **Main host: Hetzner Helsinki (owner decision, confirmed).**
   - Contabo EU saves only ~$2–20 a month at any load level and is ~20 ms closer.
   - In exchange it brings weaker and less consistent performance, an 80 Mbit/s object store that would throttle uncached asset delivery and restores, fair-use throttling and slower support.
   - Contabo Singapore is the only option that removes the ~175 ms. Its Core VPS plans are out of stock, though, and the VDS layouts cost more than Helsinki at 1k and 10k DAU.
   - Revisit Contabo Singapore only if the Helsinki staging measurements (E18-10) show the latency hurts lesson completion.
2. **Backups and DR: Hetzner Object Storage in Falkenstein or Nuremberg (owner-accepted 2026-11-17).** This covers `pg_dump`, the WAL archive, the deletion ledger with retention lock, and a nightly versioned copy of `lesson-assets`.
   - It is off-site from Helsinki at no extra base fee.
   - Transfer is free, and restores run at Gbit speed.
3. **Rejected by owner 2026-11-17: a provider-independent third copy** of the age-encrypted dumps only on Contabo Object Storage EU, for $2.99 per 250 GB. It covers loss of the Hetzner account, at the cost of a slow restore.
4. **Privacy notice.**
   - All learner data stays in the EU: the app in Finland, backups in Germany, served through Cloudflare. The owner approved this wording on 2026-11-17.
   - Contabo Singapore for backups would have added Singapore as a second storage country.

### 10.9 Cutover: plan A, plan B and the decision point

**Change to plan A step 3 (§7).** Railway Postgres must not be used from Helsinki for longer than the switchover. Its region is not yet verified, and a Singapore or US database adds ~100–175 ms to every query. Compute and the database therefore move together:

1. **Standby.** The Hetzner stack runs in standby: gate closed, River off, pointed at the Hetzner subscriber database.
2. **Hold and sync.** At switchover the Railway API's gate holds new work, sets Railway read-only, waits for subscriber LSN = publisher LSN and syncs sequences.
3. **Open Hetzner.** The Hetzner gate opens and starts River. The Cloudflare origin then switches to Hetzner, which takes seconds on a proxied record.
4. **Drain Railway.** The Railway API swaps its pool to the Hetzner database, so held requests complete, cross-region, inside the 15 s window. Railway compute then scales to zero.

If Railway turns out to be in the EU, the original step 3 (compute first) may be used instead.

**Plan B: read-only window of at most 15 minutes (owner-accepted fallback).**

| Step | Action | User impact |
|---|---|---|
| B0 | Rehearse on Helsinki staging with a production-sized copy. The full sequence must finish in ≤ 12 min (3 min margin), or plan B is re-planned before prod. Schedule the window in the lowest-traffic hour, away from streak rollover. | none |
| B1 | The gate's **read-only mode** turns on (Railway API): reads and lesson playback continue. Writes return `503` with `Retry-After`, the client shows "saving paused" and retries, and River soft-stops. Then `ALTER DATABASE … SET default_transaction_read_only = on` and terminate other sessions. | writes paused |
| B2 | `pg_dump -Fd -j N` from Railway (TCP proxy, TLS) → `pg_restore -j N` into Hetzner. Sequences travel with the dump. | writes paused |
| B3 | `infra:pg-replication-check --compare` checks row counts per table and a checksum sample. Then `ANALYZE`. | writes paused |
| B4 | Open the Hetzner stack, switch the Cloudflare origin and scale Railway compute to zero. | writes resume |
| Abort | If B3 has not passed by minute 13: clear read-only on Railway, stay on Railway, and re-plan. | ≤ 15 min of paused writes |
| Rollback (30 days) | Railway is kept intact at the cutover point. Rollback is the reverse dump/restore in another ≤ 15 min read-only window. Optionally, reverse logical replication can run from Hetzner (where we control `wal_level`) to Railway if the Railway role can subscribe. | ≤ 15 min of paused writes |

**Decision point.**
- **Provisional (end of E18-01 preflight):** plan A if all of these hold:
  1. `wal_level` is already `logical`, or the owner accepts one restart via `ALTER SYSTEM`, which ends Railway database support, and the value survives the restart;
  2. the role has REPLICATION (or superuser) and can `CREATE PUBLICATION`;
  3. the TCP proxy is reachable from Helsinki;
  4. the PK/replica-identity audit is clean or fixable.

  Otherwise plan B.
- **Final (E18-10 rehearsal):** plan A needs a passing staging rehearsal, at most two attempts. Otherwise plan B.

The owner records the choice in `docs/infra/cutover-runbook.md`.

### 10.10 Phased plan changes

The §8 phases still apply, with these changes:
- "SIN" becomes **HEL**.
- Phase 1 builds the gate's read-only mode and the dump/restore path alongside plan A.
- Phase 2 measures client-side latency from a Southeast Asian vantage point and rehearses the chosen cutover path.
- RustFS is used only as the conformance fallback.

### 10.11 Open items

1. Railway Postgres facts: `wal_level`, REPLICATION, region, and TCP proxy reachability from Helsinki. These decide plan A or plan B (E18-01).
2. ~~Backup/DR location~~: decided 2026-11-17. It is Hetzner Object Storage FSN/NBG, with no Contabo copy and EU privacy wording approved (#251).
3. Argo: the owner decides after the staging latency data (E18-10).
4. Hetzner CX availability at order time (it saves ~$9 a month at 100 DAU).
5. Revisit R2 only if the versioning code is rewritten.

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
- Hetzner Singapore (read 2026-11-17): locations and products https://docs.hetzner.com/cloud/general/locations/ , Singapore page and traffic https://www.hetzner.com/cloud-singapore/ , billing FAQ https://docs.hetzner.com/cloud/billing/faq/ , plan prices via https://website-price-api.hetzner.com/api/v1/products/CLOUD_141 … CLOUD_149 (backs https://www.hetzner.com/cloud/regular-performance/ and https://www.hetzner.com/cloud/general-purpose/), volumes https://docs.hetzner.com/cloud/volumes/overview/
- Railway Postgres (read 2026-11-17): https://railway.com/deploy/postgres , https://github.com/railwayapp-templates/postgres-ssl , support scope https://docs.railway.com/databases#database-support-scope , TCP proxy https://docs.railway.com/networking/tcp-proxy , HA https://docs.railway.com/databases/postgresql-ha
- PostgreSQL 18 logical replication (read 2026-11-17): restrictions https://www.postgresql.org/docs/18/logical-replication-restrictions.html , config https://www.postgresql.org/docs/18/logical-replication-config.html , security https://www.postgresql.org/docs/18/logical-replication-security.html , monitoring https://www.postgresql.org/docs/18/logical-replication-monitoring.html , CREATE SUBSCRIPTION https://www.postgresql.org/docs/18/sql-createsubscription.html , `default_transaction_read_only` https://www.postgresql.org/docs/18/runtime-config-client.html
- River (read 2026-11-17): leader election https://riverqueue.com/docs/leader-election , graceful shutdown https://riverqueue.com/docs/graceful-shutdown , drivers https://riverqueue.com/docs/database-drivers
- Hetzner Helsinki and add-ons (read 2026-11-17): price service https://website-price-api.hetzner.com/api/v1/products/CLOUD_133 (CX33), CLOUD_141–144 (CPX12–42), CLOUD_147–148 (CCX13–23), CLOUD_21 (IPv4), CLOUD_66 (traffic), CLOUD_84 (Object Storage base); plan pages https://www.hetzner.com/cloud/cost-optimized/ , https://www.hetzner.com/cloud/regular-performance/ , https://www.hetzner.com/cloud/general-purpose/ ; volumes and snapshots https://www.hetzner.com/cloud/block-storage/ ; Object Storage pricing https://www.hetzner.com/storage/object-storage/pricing/ , overview (base price per account, free `eu-central` traffic, limits) https://docs.hetzner.com/storage/object-storage/overview , supported actions https://docs.hetzner.com/storage/object-storage/supported-actions
- Latency (read 2026-11-17; data 2026-11-02 to 2026-11-16): https://wondernetwork.com/pings/Helsinki/Singapore and the matching `/pings/<A>/<B>` pages for Kuala Lumpur, Jakarta, Bangkok, Manila, Hanoi, Ho Chi Minh City, Frankfurt and Nuremberg
- Cloudflare (read 2026-11-17): network https://www.cloudflare.com/network/ , Tiered Cache https://developers.cloudflare.com/cache/how-to/tiered-cache/ , connection limits (Proxy Read Timeout 125 s, origin keep-alive) https://developers.cloudflare.com/fundamentals/reference/connection-limits/ , Connection Reuse https://developers.cloudflare.com/smart-shield/concepts/connection-reuse/ , Smart Shield https://developers.cloudflare.com/smart-shield/get-started/ , plans https://www.cloudflare.com/plans/ , Argo https://www.cloudflare.com/application-services/products/argo-smart-routing/
- Contabo (read 2026-11-17): pricing https://contabo.com/en/pricing/ , VPS https://contabo.com/en/vps/ and configurators such as https://contabo.com/en/vps/cloud-vps-core-6/ , VDS https://contabo.com/en/vds/ and https://contabo.com/en/vds/vds-s/ , locations https://contabo.com/en/locations/ and https://contabo.com/en/locations/asia-singapore/ , Object Storage https://contabo.com/en/object-storage/ , endpoints https://help.contabo.com/en/support/solutions/articles/103000411092 , versioning https://help.contabo.com/en/support/solutions/articles/103000282907 , Object Lock https://help.contabo.com/en/support/solutions/articles/103000282887 , limits https://help.contabo.com/en/support/solutions/articles/103000411100 , terms (SLA clause 11) https://contabo.com/en/legal/terms-and-conditions/ , Auto Backup https://contabo.com/en/auto-backup/ , private networking https://contabo.com/en/private-networking/ , DDoS https://contabo.com/en/ddos-protection/ , support https://contabo.com/en/support-experience/
- Benchmarks and incidents (third-party, read 2026-11-17): https://www.experte.com/server/vps-benchmark (updated 2026-01-07), https://www.vpsbenchmarks.com/compare/contabo_vs_hetzner , https://www.vpsbenchmarks.com/trials/hetzner_performance_trial_28Aug2026/sysbench , https://www.vpsbenchmarks.com/trials/contabo_performance_trial_27Jul2026/sysbench , https://pingoru.io/providers/contabo/outage-history
