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

Keep security updates on, set `Unattended-Upgrade::Automatic-Reboot "true"` with `Automatic-Reboot-Time "04:00"` (UTC+0; the backup jobs run at 02:30), and make sure Docker restarts services (`restart: unless-stopped` is set).

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
- Use separate Object Storage keys: the application token (Object Read & Write, scoped to the application bucket) and a backup token (Object Read & Write scoped to the backup bucket only; R2 has no delete-less permission, so protect `wal/` and `base/` with bucket lock rules below). Retention of `wal/` and `base/` is a bucket lifecycle rule applied with an admin key that is not on the VM: `BACKUP_ADMIN_S3_ACCESS_KEY_ID`/`BACKUP_ADMIN_S3_SECRET_ACCESS_KEY` plus `BACKUP_S3_ENDPOINT/REGION/BUCKET` in the environment, then `mise run infra:backup-lifecycle`. It sets `base/` to expire after 9 days and `wal/` after 10 (for the 7-day PITR window), noncurrent versions after 1 day (only when `BACKUP_S3_VERSIONING` is not `none`; R2 has no bucket versioning, so set `none` there and `Expiration` deletes for good) and aborts incomplete uploads. **R2 bucket locks:** S3 cannot set them, so as the owner run `npx wrangler r2 bucket lock add <backup-bucket> ...` (or Dashboard > bucket > Settings > Bucket lock rules) with an Age rule of 10 days on prefix `wal/` and 9 days on `base/` (retention equal to the lifecycle expiry; a locked object cannot be overwritten or deleted by a stolen VM token). Do not lock `dr/`: a changed asset is re-copied over its key. Check the exact wrangler flags in the current Cloudflare docs first. Reasoning: to recover to any time in the last 7 days you need the base taken just before it (nightly, plus one missed night = 9 days), and every WAL segment after that base, so `wal/` must expire strictly later than `base/`; a retained base then always has its WAL. The lifecycle cannot protect the newest base if the base job stops: that is the `DuolifyBackupStale` / `DuolifyNoRecentBaseInBucket` alert (36 h), long before `base/` drains.
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

**No 502s while an instance stops.** The `/v1` `reverse_proxy` sets `lb_try_duration 5s` and `lb_try_interval 250ms` (Caddy's retries are off by default). `api` is one static upstream; Docker DNS answers with every live replica and Caddy resolves it on each dial (no `dynamic a` upstream, so no 1 m refresh cache), and a stopped replica drops out of DNS. Per the Caddy docs, a failed connection is always retried inside the 5 s window, whatever the method. A request that connected but failed mid-round-trip is retried only if it matches `lb_retry_match`, which defaults to GET alone: a POST cut off mid-request is not replayed and the client sees a 502. **A POST is only safe if the instance drains instead of dying:** `deploy.sh` stops an instance with `docker stop` (SIGTERM). The API (`httpapi.Serve`) and worker (`worker.Shutdown`) stop accepting, finish in-flight requests and jobs for up to 10 s, then exit; `stop_grace_period: 30s` in `compose.prod.yml` keeps Docker from SIGKILLing them first (the default 10 s equals the drain time). A SIGKILL (crash, OOM) still cuts requests in flight on that instance; only GETs are replayed. `mise run infra:overlay-rolling` stops each API instance in turn the way `deploy.sh` does, under 8 GET and 4 POST clients plus 6 slow GETs (2 s stream) and 6 slow POSTs (body arrives over 3 s) in flight on each instance, and requires zero failed GETs, POSTs and slow requests (`ROLLING_NEGATIVE_CONTROL=1` SIGKILLs instead, to show the test detects cut requests). It then points the proxy at two stub replicas, one of which drops connections, and requires every GET to be answered (0 of 40 fail with the `lb_try_duration` setting, 20 of 40 without). The retry window never touches an open stream: it only applies while a request is picking an upstream, and `infra:overlay-sse` still holds a 165 s stream.

**Long SSE streams.** The `/v1` route never buffers (`flush_interval -1`, and Caddy flushes `text/event-stream` anyway) and sets no upstream `read_timeout`/`write_timeout`, no server `write` timeout and no `stream_timeout`, so the proxy only closes a stream when either side does; the API must keep its own write deadline alive and send a `: keepalive` comment every `SSE_HEARTBEAT_INTERVAL` (15 s) so that the 20 s server `WriteTimeout` and any client or carrier idle timeout never fire. `mise run infra:overlay-sse` holds a stream open for ~177 s with 10-15 s silences and fails if an event arrives more than 2 s after it was sent or the stream is cut.
