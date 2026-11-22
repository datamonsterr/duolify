# VM hardening runbook

Applies to the Hetzner VM that runs the [production overlay](selfhost.md#production-overlay-hel1). Do these once, before the first deploy, in this order. Nothing here is applied by CI.

## 1. Firewall: Cloudflare only

Only Cloudflare may reach ports 80/443; SSH comes from your admin address only. Use a Hetzner Cloud Firewall (preferred, outside the VM) and keep the same rules in `ufw`.

- Inbound 443 (and 80 for redirects): the ranges at <https://www.cloudflare.com/ips-v4> and <https://www.cloudflare.com/ips-v6>. Refresh them monthly or on Cloudflare's notice.
- Inbound 22/tcp: your admin IPs only. Everything else inbound: denied. Outbound: allow.
- Docker bypasses `ufw` for published ports. The compose stack publishes only the proxy; keep it that way (`infra:deploy-plan` rejects any other published port) and set `BIND_ADDRESS` to the public interface address.
- Enable Authenticated Origin Pulls in Cloudflare so a request that skips Cloudflare is refused at the origin even from an allowed range.

## 2. SSH keys only

In `/etc/ssh/sshd_config.d/10-hardening.conf`:

```
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers deploy admin
```

Create a non-root `deploy` user in the `docker` group with only the CI public key in `authorized_keys`. Give `admin` sudo. Reload with `sudo systemctl reload ssh` and test a second session before closing the first. The CI private key lives only in the GitHub environment secret `DEPLOY_SSH_KEY`; put the VM's host key line in `DEPLOY_KNOWN_HOSTS` (`ssh-keyscan -t ed25519 <host>`, verified out of band).

## 3. Unattended upgrades

```sh
sudo apt install unattended-upgrades
sudo dpkg-reconfigure -plow unattended-upgrades
```

Keep security updates on, set `Unattended-Upgrade::Automatic-Reboot "true"` with `Automatic-Reboot-Time "04:00"` (UTC+0; the worker dump runs at 02:00 and the walship base backup and DR copy start at 03:00, so the heavy jobs are an hour apart and finish before the reboot), and make sure Docker restarts services (`restart: unless-stopped` is set).

## 4. Docker daemon

`/etc/docker/daemon.json`:

```json
{
  "log-driver": "json-file",
  "log-opts": {"max-size": "10m", "max-file": "5"},
  "live-restore": true,
  "no-new-privileges": true,
  "userland-proxy": false,
  "icc": true
}
```

`icc` stays on: Compose networks rely on it. Do not expose the Docker socket or a TCP daemon port. Do not add application users to `docker` except `deploy`.

## 5. Secrets

- `infra/deploy/.env` is mode 0600, owned by `deploy`, never committed. `selfhost.sh init` generates the internal secrets; replace `S3_SECRET_ACCESS_KEY` and `BACKUP_S3_SECRET_ACCESS_KEY` with the Cloudflare R2 API token keys (R2 > Manage API tokens; endpoint `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`, region `auto`).
- Use separate Object Storage keys: the application token (Object Read & Write, scoped to the application bucket) and a backup token (Object Read & Write scoped to the backup bucket only; R2 has no delete-less permission, so protect `wal/` and `base/` with bucket lock rules below). Retention of `wal/` and `base/` is a bucket lifecycle rule applied with an admin key that is not on the VM: `BACKUP_ADMIN_S3_ACCESS_KEY_ID`/`BACKUP_ADMIN_S3_SECRET_ACCESS_KEY` plus `BACKUP_S3_ENDPOINT/REGION/BUCKET` in the environment, then `mise run infra:backup-lifecycle`. It sets `base/` to expire after 9 days and `wal/` after 10 (for the 7-day PITR window), noncurrent versions after 1 day (only when `BACKUP_S3_VERSIONING` is not `none`; R2 has no bucket versioning, so set `none` there and `Expiration` deletes for good) and aborts incomplete uploads. **Status (owner, 2026-11-22): the locks are already set on `duolify-backups` (`wal/` 10 days, `base/` 9 days) and the lack of versioning on R2 is accepted for this phase.** **R2 bucket locks:** S3 cannot set them, so as the owner run `npx wrangler r2 bucket lock add <backup-bucket> ...` (or Dashboard > bucket > Settings > Bucket lock rules) with an Age rule of 10 days on prefix `wal/` and 9 days on `base/` (retention equal to the lifecycle expiry; a locked object cannot be overwritten or deleted by a stolen VM token). Do not lock `dr/`: a changed asset is re-copied over its key. Check the exact wrangler flags in the current Cloudflare docs first. Reasoning: to recover to any time in the last 7 days you need the base taken just before it (nightly, plus one missed night = 9 days), and every WAL segment after that base, so `wal/` must expire strictly later than `base/`; a retained base then always has its WAL. The lifecycle cannot protect the newest base if the base job stops: that is the `DuolifyBackupStale` / `DuolifyNoRecentBaseInBucket` alert (36 h), long before `base/` drains.
- The age private identity (`infra/deploy/secrets/backup-age-identity.txt`) must not stay on the VM: copy it to your password manager and a second offline place, then delete it from the VM. The drill and a real restore need it back. Without it, no backup or WAL archive can be read.
- CI holds only the four `DEPLOY_*` secrets. GHCR packages are public or the VM is logged in with a read-only token (`docker login ghcr.io`).

## 6. After the first boot

1. `bash infra/deploy/deploy-plan.sh` passes (run `mise run infra:deploy-plan` from a checkout).
2. `sudo ss -tlnp` shows only 22, 80 and 443 listening publicly; `docker compose ps` lists no published port other than the proxy; `curl http://<vm-ip>:50051` from outside fails.
3. A request that goes straight to the VM IP is refused (firewall, then Authenticated Origin Pulls).
4. `selfhost.sh smoke` passes and two `api` containers are healthy.
5. WAL is flowing: `select failed_count, last_archived_wal from pg_stat_archiver;` shows no failures, and `wal/` in the backup bucket grows. Run `docker compose exec walship /walship.sh base`, then `selfhost.sh pitr-drill` with a time a few minutes ago.
6. The backup bucket is outside the VM and the app bucket (`BACKUP_LOCATION`, Cloudflare R2 here; versioned unless `BACKUP_S3_VERSIONING=none`), and passes `infra:s3-conformance` when that task exists; `mise run infra:backup-preflight` passes (versioning when applicable, lifecycle as above, a base newer than the PITR window exists; run it after the first nightly base or `walship.sh base`); the privacy notice lists the storage country.
7. Prometheus (`docker compose exec prometheus wget -qO- localhost:9090/api/v1/rules`) lists the `duolify-data-protection` rules, none firing; point an Alertmanager or notifier at it.
8. Reboot the VM once: the stack returns without manual steps and the unattended-upgrade timer is active (`systemctl list-timers apt-daily-upgrade`).

## 7. Deploys

`deploy.sh` (run by `deploy-vm.yml`) deploys digest-pinned images only: `api=<repo>@sha256:…` for `api worker engine web backup-init`. Tags are refused.

**Migrations must be expand/contract.** `deploy.sh` runs `/api migrate` first and only then replaces containers, so the old API keeps serving against the new schema during the whole roll, and keeps doing so if the deploy fails afterwards. Therefore a migration may only add things the old code ignores (new tables, nullable or defaulted columns, new indexes created concurrently). Dropping or renaming a column, tightening a constraint, or changing a type is a second release, after the code that stopped using it is live everywhere. There is no automatic down-migration.

**State after a failed deploy.**
- Digest pull or `walship` build fails: nothing changed except the image variables in `.env`; the running stack is untouched.
- `migrate` fails: goose runs each file in a transaction unless it opts out, so earlier files stay applied and the failing one is rolled back (check any `NO TRANSACTION` file by hand); containers are untouched, the old version keeps serving. Fix forward and redeploy.
- Engine fails its healthcheck: new Engine container is unhealthy; the API instances are still the old ones. Redeploy the previous digests to roll the Engine back (migrations stay applied, hence expand/contract).
- An API instance fails to become healthy: that step stops with the old instance still running (the new one starts first and the old one is stopped only after it is healthy). The rest of the fleet is old; the migration is applied. Redeploy the previous digests.
- Worker or proxy fails: API and Engine are new, worker or edge may be old; redeploy.
Rollback is always "run `deploy.sh` with the previous commit's digests"; a Postgres restore is only for data loss, not for a bad deploy.

**Restart order: Engine, then API instances, then worker, then proxy.** The API is the Engine's gRPC client, so the server goes first: a new Engine must keep serving the old API's calls and the new API then finds the RPCs it needs. That makes `proto` changes additive across one release (add fields/RPCs first; remove only in a later release after no client uses them), consistent with the rule that `proto` changes land together in `services/api` and `engine`. The mixed window (new Engine, old API) lasts until the last API instance is replaced; the Engine is not scaled, so during its restart requests that need it fail or retry. The worker restarts after the API so its jobs run the new code against the already migrated schema.

**A failed deploy converges.** `deploy.sh` removes leftovers of earlier failed deploys before it judges health: containers of `api`, `worker`, `engine` and `proxy` that are restarting, dead, exited with an error or unhealthy (otherwise `up --wait` fails on "container ...-api-3 is unhealthy" for a crash loop that has nothing to do with this deploy). If any later step fails, it removes the new API containers that never became healthy (old ones are stopped only after their replacement is healthy, so the previous generation keeps serving), removes any broken container, and restores the previous image references in `.env`. Postgres, Redis, walship, Prometheus, node-exporter and all volumes are never removed (`docker rm` without `-v`). Covered by `test-deploy.sh` (stateful fake `docker`: leftover crash loop, failed rollout, crash-looping worker).

**Images carry a CA bundle.** The worker image is built from `postgres:18`, which ships no root certificates, so every outbound HTTPS call (Cloudflare R2, Clerk, PostHog) failed TLS verification on staging while CI, which uses a plain-HTTP local object store, stayed green. The worker and walship images now copy `/etc/ssl/certs/ca-certificates.crt` from the build stage; `infra/deploy/test-image-ca.sh` (`mise run infra:overlay-ca`, overlay job) asserts the bundle exists, has at least 100 kB and 100 certificates and contains ISRG Root X1 in the api, worker, engine and walship images. A store failure is logged once at error level with `op`, `bucket`, `key_prefix`, `error_class` (`x509`, `timeout`, `http_403` ...), `http_status` and `s3_code`; the returned error carries only the class.

**No 502s while an instance stops.** The `/v1` `reverse_proxy` sets `lb_try_duration 5s` and `lb_try_interval 250ms` (Caddy's retries are off by default). `api` is one static upstream; Docker DNS answers with every live replica and Caddy resolves it on each dial (no `dynamic a` upstream, so no 1 m refresh cache), and a stopped replica drops out of DNS. Per the Caddy docs, a failed connection is always retried inside the 5 s window, whatever the method. A request that connected but failed mid-round-trip is retried only if it matches `lb_retry_match`, which defaults to GET alone: a POST cut off mid-request is not replayed and the client sees a 502. **A POST is only safe if the instance drains instead of dying:** `deploy.sh` stops an instance with `docker stop` (SIGTERM). The API (`httpapi.Serve`) and worker (`worker.Shutdown`) stop accepting, finish in-flight requests and jobs for up to 10 s, then exit; `stop_grace_period: 30s` in `compose.prod.yml` keeps Docker from SIGKILLing them first (the default 10 s equals the drain time). A SIGKILL (crash, OOM) still cuts requests in flight on that instance; only GETs are replayed. `mise run infra:overlay-rolling` stops each API instance in turn the way `deploy.sh` does, under 8 GET and 4 POST clients plus 6 slow GETs (2 s stream) and 6 slow POSTs (body arrives over 3 s) in flight on each instance, and requires zero failed GETs, POSTs and slow requests (`ROLLING_NEGATIVE_CONTROL=1` SIGKILLs instead, to show the test detects cut requests). It then points the proxy at two stub replicas, one of which drops connections, and requires every GET to be answered (0 of 40 fail with the `lb_try_duration` setting, 20 of 40 without). The retry window never touches an open stream: it only applies while a request is picking an upstream, and `infra:overlay-sse` still holds a 165 s stream.

**Long SSE streams.** The `/v1` route never buffers (`flush_interval -1`, and Caddy flushes `text/event-stream` anyway) and sets no upstream `read_timeout`/`write_timeout`, no server `write` timeout and no `stream_timeout`, so the proxy only closes a stream when either side does; the API must keep its own write deadline alive and send a `: keepalive` comment every `SSE_HEARTBEAT_INTERVAL` (15 s) so that the 20 s server `WriteTimeout` and any client or carrier idle timeout never fire. `mise run infra:overlay-sse` holds a stream open for ~177 s with 10-15 s silences and fails if an event arrives more than 2 s after it was sent or the stream is cut.

## 8. Sizing (cx23: 2 vCPU, 4 GB, 40 GB)

`infra:deploy-plan` enforces the memory budget (limits of the services that run on the VM, replicas included, at most 3584 MiB; `DEPLOY_MEMORY_BUDGET_MB` overrides it for a bigger VM) and the connection budget below. `deploy.sh` runs it against `.env` before it pulls anything.

| Service | Limit | Reservation | Notes |
|---|---|---|---|
| postgres | 1280 MiB | 768 MiB | `POSTGRES_MEMORY`; shared_buffers 384 MB leaves about 900 MB for backends, autovacuum and WAL buffers |
| api x 2 | 384 MiB each (768) | 128 MiB each | `GOMEMLIMIT=300MiB`; a roll briefly runs 3 |
| worker | 384 MiB | 128 MiB | `GOMEMLIMIT=300MiB` |
| engine | 384 MiB | 96 MiB | Rust, stateless |
| walship | 256 MiB | 64 MiB | `pg_basebackup`, gzip, age and the `aws` CLI |
| prometheus | 192 MiB | 96 MiB | 15 days of a handful of series |
| proxy (Caddy) | 96 MiB | 32 MiB | |
| redis | 96 MiB | 32 MiB | `maxmemory 64mb` |
| node-exporter | 48 MiB | 16 MiB | |
| **Total** | **3504 MiB** | **1488 MiB** | about 590 MiB of the 4096 MiB stays for the kernel, dockerd, sshd and page cache |

The limits are caps, not a forecast: the real use of the API, worker and engine is far below them. Measure with `docker stats --no-stream` after a day of traffic and tighten or raise from there. A 1-2 GB swap file on the VM (with `vm.swappiness=10`) is a cheap safety net: the kernel pages out idle memory instead of picking an OOM victim during the nightly jobs.

**Two API replicas.** Kept, but not for the rolling restart: `deploy.sh` starts a new instance, waits for it to be healthy and then stops an old one, which is zero-downtime from a single replica as well. The second replica keeps the API serving when one instance is killed (OOM, crash) and is what `infra:overlay-rolling` and the Caddy retry rule exercise. It costs about 100-150 MiB of real memory and doubles the background loops that every API process runs (the loops are guarded by advisory locks; making them cheaper belongs to the API work, not to this runbook). With no load numbers for this box, drop to one replica (`replicas: 1` in `compose.prod.yml`) only if memory is short or CPU stays saturated; the plan accepts either.

**Postgres** (`compose.prod.yml`), chosen for a small SSD-backed OLTP database that also archives WAL:

| Setting | Value | Why |
|---|---|---|
| `shared_buffers` | 384MB | about 30% of the container limit; the working set is small, the OS cache does the rest |
| `effective_cache_size` | 1GB | planner hint only: this buffer plus the share of the OS cache the data can expect |
| `work_mem` | 8MB | per sort or hash node; 60 connections would not reach the limit in practice |
| `maintenance_work_mem` | 64MB | explicit; autovacuum workers use it, 3 workers at most |
| `max_connections` | 60 | pools below plus 8 spare; each backend costs memory |
| `wal_compression` | lz4 | smaller WAL means less to ship to R2, cheap in CPU |
| `checkpoint_timeout` / `max_wal_size` / `min_wal_size` | 15min / 2GB / 256MB | fewer, smoother checkpoints (`checkpoint_completion_target=0.9`) |
| `random_page_cost` / `effective_io_concurrency` | 1.1 / 200 | SSD |
| `jit` | off | compile cost outweighs the gain for short OLTP queries |
| `max_parallel_workers_per_gather` | 1 | 2 vCPU |
| autovacuum | naptime 30s, scale factors 0.1 / 0.05, cost limit 1000 | keeps small hot tables tidy without long sleeps |
| `idle_in_transaction_session_timeout` | 300s | a leaked transaction cannot pin vacuum for long |
| `log_min_duration_statement`, `track_io_timing` | 500ms, on | find slow statements |
| `shared_preload_libraries` | `pg_stat_statements` | does not touch WAL or archiving; run `CREATE EXTENSION pg_stat_statements;` once to read it |

No global `statement_timeout`: migrations and `pg_basebackup` must not be cut. Change one setting at a time and compare with `mise run infra:load-baseline`. Settings that need a restart (`shared_buffers`, `max_connections`, `shared_preload_libraries`) only apply when the container is recreated, which `deploy.sh` never does for data services (`--no-recreate`). After a change to the Postgres, Redis, walship, Prometheus or node-exporter definition, recreate them once in a quiet moment, ideally right after the 03:00 job has finished:

```sh
cd /opt/duolify/infra/deploy
SELFHOST_OVERLAY=$PWD/compose.prod.yml bash selfhost.sh compose up --detach --wait postgres redis walship node-exporter prometheus
```

Postgres restarts for a few seconds (the API retries; WAL archiving resumes from the spool).

**Connection budget.** Every process has its own pgx pool (`DB_POOL_*`, `internal/store/pool.go`): API 10 (min 2) per replica, worker 16 (min 4; its River clients each hold a LISTEN connection), the one-shot `migrate` 2. With 2 replicas plus one more during a roll: 3 x 10 + 16 + 8 spare (3 reserved superuser slots, walship, an operator) = 54, under `max_connections=60`. Override with `API_DB_POOL_MAX_CONNS`, `WORKER_DB_POOL_MAX_CONNS` and the `_MIN_` pair in `.env`; `deploy-plan` rejects a total over `max_connections`.

**Redis** (`compose.yml`) is a pure cache: the wallet cache (5 min TTL) and the league ranking (TTL, rebuilt from Postgres when missing) are its only users, and `infra:overlay-redis` proves a flush loses nothing. So `maxmemory 64mb` with `allkeys-lru` (evict instead of failing writes), and no RDB or AOF (`--save "" --appendonly no`): a restart starts empty and the keys refill.

**Backup schedule.** The worker's encrypted database dump runs at 02:00 UTC (and once at every worker start). The walship base backup and the DR copy start at 03:00 UTC, an hour later, so a `pg_dump | age` and a `pg_basebackup | gzip | age` plus a bucket scan never compete for the two CPUs, and both still finish before the 04:00 reboot. The staleness alerts use 36 h windows, so the move changes no alert.

**Caddy.** `encode zstd gzip` is on for the whole site with Caddy's default response matcher, which compresses text, JSON, JavaScript, SVG, fonts and wasm and leaves images and archives alone. Event streams (`text/event-stream`) are never held back by the minimum length and are flushed per write (`flush_interval -1`); `infra:overlay-sse` runs with `--compressed` and requires every event within 2 s.


The API's application identity also deletes generated lesson objects after their retention window. For versioned application buckets grant listing versions and deletion of versions under `lessons/generated/` only; keep backup writer credentials separate and deny changes to bucket policies/versioning. R2 does not support versioning or prefix-scoped tokens: its application token is scoped to the application bucket, and application code restricts the purge to generated prefixes. Set `S3_VERSIONING=none` for R2, or `enabled` for a versioned application bucket. The worker receives neither application secret; it retains its separate backup writer identity. Configure `POSTHOG_PROJECT_ID` and `POSTHOG_PERSONAL_API_KEY` for worker person deletion.
