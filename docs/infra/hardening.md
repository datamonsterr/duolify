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
- Use separate Object Storage keys: the application key (read/write on the application bucket in `hel1`) and a backup key (put/get/list on the backup bucket; no delete). Retention and the 7-day expiry of `wal/` and `base/` run as a bucket lifecycle rule configured with an admin key that is not on the VM.
- The age private identity (`infra/deploy/secrets/backup-age-identity.txt`) must not stay on the VM: copy it to your password manager and a second offline place, then delete it from the VM. The drill and a real restore need it back. Without it, no backup or WAL archive can be read.
- CI holds only the four `DEPLOY_*` secrets. GHCR packages are public or the VM is logged in with a read-only token (`docker login ghcr.io`).

## 6. After the first boot

1. `bash infra/deploy/deploy-plan.sh` passes (run `mise run infra:deploy-plan` from a checkout).
2. `sudo ss -tlnp` shows only 22, 80 and 443 listening publicly; `docker compose ps` lists no published port other than the proxy; `curl http://<vm-ip>:50051` from outside fails.
3. A request that goes straight to the VM IP is refused (firewall, then Authenticated Origin Pulls).
4. `selfhost.sh smoke` passes and two `api` containers are healthy.
5. WAL is flowing: `select failed_count, last_archived_wal from pg_stat_archiver;` shows no failures, and `wal/` in the backup bucket grows. Run `docker compose exec walship /walship.sh base`, then `selfhost.sh pitr-drill` with a time a few minutes ago.
6. The backup bucket is in `BACKUP_LOCATION` (not `hel1`), versioned, and passes `infra:s3-conformance` when that task exists; the lifecycle rule for `wal/` and `base/` is in place; the privacy notice lists the storage country.
7. Prometheus (`docker compose exec prometheus wget -qO- localhost:9090/api/v1/rules`) lists the `duolify-data-protection` rules, none firing; point an Alertmanager or notifier at it.
8. Reboot the VM once: the stack returns without manual steps and the unattended-upgrade timer is active (`systemctl list-timers apt-daily-upgrade`).
