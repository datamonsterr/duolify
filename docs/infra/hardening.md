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

- `infra/deploy/.env` is mode 0600, owned by `deploy`, never committed. `selfhost.sh init` generates the internal secrets; replace `S3_SECRET_ACCESS_KEY` and `BACKUP_S3_SECRET_ACCESS_KEY` with the Hetzner Object Storage keys.
- Use separate Object Storage keys: the application key (read/write on the application bucket in `hel1`) and a backup key (put/get/list on the backup bucket; no delete). Retention of `wal/` and `base/` is a bucket lifecycle rule applied with an admin key that is not on the VM: `BACKUP_ADMIN_S3_ACCESS_KEY_ID`/`BACKUP_ADMIN_S3_SECRET_ACCESS_KEY` plus `BACKUP_S3_ENDPOINT/REGION/BUCKET` in the environment, then `mise run infra:backup-lifecycle`. It sets `base/` to expire after 9 days and `wal/` after 10 (for the 7-day PITR window), noncurrent versions after 1 day (without that, a versioned bucket never frees space) and aborts incomplete uploads. Reasoning: to recover to any time in the last 7 days you need the base taken just before it (nightly, plus one missed night = 9 days), and every WAL segment after that base, so `wal/` must expire strictly later than `base/`; a retained base then always has its WAL. The lifecycle cannot protect the newest base if the base job stops: that is the `DuolifyBackupStale` / `DuolifyNoRecentBaseInBucket` alert (36 h), long before `base/` drains.
- The age private identity (`infra/deploy/secrets/backup-age-identity.txt`) must not stay on the VM: copy it to your password manager and a second offline place, then delete it from the VM. The drill and a real restore need it back. Without it, no backup or WAL archive can be read.
- CI holds only the four `DEPLOY_*` secrets. GHCR packages are public or the VM is logged in with a read-only token (`docker login ghcr.io`).

## 6. After the first boot

1. `bash infra/deploy/deploy-plan.sh` passes (run `mise run infra:deploy-plan` from a checkout).
2. `sudo ss -tlnp` shows only 22, 80 and 443 listening publicly; `docker compose ps` lists no published port other than the proxy; `curl http://<vm-ip>:50051` from outside fails.
3. A request that goes straight to the VM IP is refused (firewall, then Authenticated Origin Pulls).
4. `selfhost.sh smoke` passes and two `api` containers are healthy.
5. WAL is flowing: `select failed_count, last_archived_wal from pg_stat_archiver;` shows no failures, and `wal/` in the backup bucket grows. Run `docker compose exec walship /walship.sh base`, then `selfhost.sh pitr-drill` with a time a few minutes ago.
6. The backup bucket is in `BACKUP_LOCATION` (not `hel1`), versioned, and passes `infra:s3-conformance` when that task exists; `mise run infra:backup-preflight` passes (versioned, lifecycle as above, a base newer than the PITR window exists; run it after the first nightly base or `walship.sh base`); the privacy notice lists the storage country.
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

**No 502s while an instance stops.** The Caddy upstream retries connection failures for up to 5 s (`lb_try_duration`), and `api` resolves to every replica; `mise run infra:overlay-rolling` kills each instance in turn under GET+POST load and expects no proxy error. A request that already reached a dying instance is retried only if it is a GET; a POST is never replayed.
