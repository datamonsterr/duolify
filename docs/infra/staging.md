# Staging environment

Staging runs the [production overlay](selfhost.md#production-overlay-hel1) on one Hetzner VM. Cloudflare sits in front of it. It uses live providers (OpenRouter, ElevenLabs, Resend, PostHog, Cloudflare R2). It was provisioned on 2026-11-22.

## What exists

| Resource | Value |
|---|---|
| Hetzner server | `duolify-staging`, type **cx23** (x86, 2 vCPU / 4 GB / 40 GB), location **fsn1** (Falkenstein), Ubuntu 24.04, labels `env=staging,app=duolify` |
| Cost | cx23 €6.49/month plus the primary IPv4 (Hetzner bills it separately, about €0.50/month). Traffic: 20 TB included |
| Hetzner firewall | `duolify-staging`: inbound 22/tcp from anywhere (key-only SSH; GitHub-hosted runners have no fixed addresses), 80 and 443/tcp only from Cloudflare's published ranges (<https://www.cloudflare.com/ips-v4>, `ips-v6`). Everything else is dropped. `ufw` on the VM allows 22, 80 and 443 |
| Hetzner SSH key | `duolify-staging-deploy` (the deploy public key; it was injected at creation, then removed from root by cloud-init) |
| DNS (Cloudflare, proxied) | `staging.duolify.online` A → VM, `staging-api.duolify.online` A → VM |
| TLS | Cloudflare Origin CA certificate (ECC, both host names, valid until 2029-11-21) loaded by Caddy from `infra/deploy/site.d/` on the VM. The zone is Full (strict) |
| GitHub | environment `staging` with secrets `DEPLOY_USER`, `DEPLOY_SSH_KEY`, `DEPLOY_KNOWN_HOSTS`; repo secret `HCLOUD_TOKEN` |
| Object storage | Cloudflare R2 buckets `duolify-assets` (app) and `duolify-backups` (dumps, WAL, base backups, DR copy) |

**Location deviation.** ADR 0014 picks `hel1`. Staging runs in `fsn1` because no x86 type inside the €10/month staging budget was in stock in hel1 on 2026-11-22: cx23 and cx33 were unavailable there, and the cheapest available type, cpx22, costs €19.49. Part of the reason for hel1 was Hetzner Object Storage, and storage is R2 in this phase. The production location, and the ARM option (cax11, €6.99 in hel1, which needs arm64 images from `pr-ready`), are still open for the owner.

## Layout on the VM

- Users: `admin` (sudo, the owner's workstation key) and `deploy` (in group `docker`, the CI deploy key only). Root login and password login are off (`/etc/ssh/sshd_config.d/10-hardening.conf`).
- Hardening as in [hardening.md](hardening.md): unattended upgrades with a 04:00 UTC automatic reboot, and the Docker `daemon.json` from that runbook.
- `/opt/duolify/infra/` is a copy of the repository's `infra/`, written on every deploy.
- `/opt/duolify/infra/deploy/.env` holds the runtime settings: mode 0600, owner `deploy`.
- `/opt/duolify/infra/deploy/site.d/` holds `tls.caddy`, `origin.pem`, `origin.key` and `origin-ca.pem`.
- Both host names are served by the one Caddy site. The web client is on `/` and the API on `/v1` (SSE unbuffered). `PUBLIC_ORIGIN` is `https://staging.duolify.online`.

## Security headers

Caddy sends them on the web host and the API host names; see [selfhost.md](selfhost.md#security-headers) and ADR 0022. The asset host (`LESSON_ASSET_ORIGIN`, R2 custom domain) needs its own response headers in Cloudflare: the table in selfhost.md lists them. `deploy.sh` smoke fails the deploy when the proxy's headers are wrong.

## Runtime settings (names only)

- `DUOLIFY_ENV=staging`. Every check outside `development` and `test` is fail-closed.
- `PRIMARY_LOCATION=fsn1`, `BACKUP_LOCATION=cloudflare-r2`.
- `S3_*` and `BACKUP_S3_*` point at R2 (`S3_VERSIONING=none`, `BACKUP_S3_VERSIONING=none`). The API deletes private generated objects using the application identity; the worker holds only backup credentials.
- `ENGINE_PROVIDER_MODE=live` with `OPENROUTER_API_KEY`. The route is set by `engine/config/models.toml`.
- `RESEND_API_KEY` and `AUTH_MAIL_FROM` for sign-in mail.
- `POSTHOG_API_KEY`, `POSTHOG_HOST`, `POSTHOG_PROJECT_ID`, `POSTHOG_PERSONAL_API_KEY` and `ANALYTICS_OPT_OUT=false` for server analytics. The web image gets `VITE_POSTHOG_KEY` and `VITE_POSTHOG_HOST` at build time in `pr-ready`, plus `VITE_LANDING_MEDIA_BASE` from the `LANDING_MEDIA_BASE` repository variable. The landing video sits on the production asset origin, so leave the variable empty for a staging-only image (the poster shows) unless staging's `LESSON_ASSET_ORIGIN` serves `landing/v1` too.
- Narration: `NARRATION_ENABLED=true` and `NARRATION_LIVE_ENABLED=true`, with `ELEVENLABS_API_KEY`, `ELEVENLABS_VOICE_ID` and `ELEVENLABS_MODEL_ID`. The character budget is `NARRATION_MONTHLY_CHAR_LIMIT` (40000), `NARRATION_BUDGET_STOP_PERCENT` (95) and `NARRATION_CYCLE_START_DAY` (1); the compose file forwards them with those defaults. `NARRATION_LIVE_ENABLED` is forwarded to the API and worker too, because the budget is only read when it is `true`.
- The lesson test is on through the API default. The client reads it from `/v1/features`.
- Stripe, the marketplace and public character export stay off, because their variables are not set.

## Settings `deploy.sh` checks first

Before it pulls an image or migrates, `deploy.sh` runs `infra/deploy/deploy-plan.sh` against `.env` (it needs `jq` and `docker compose` on the VM) and stops with a list of what is wrong. It checks, among others:

- `BACKUP_S3_ACCESS_KEY_ID` differs from `S3_ACCESS_KEY_ID`. The API refuses to start when the backup writer reuses the application key, so create a second R2 token scoped to `duolify-backups` only.
- `BACKUP_AGE_RECIPIENT` is a real age recipient (`age1...`, 62 characters). CI never generates keys. Generate the pair **once**, on the owner workstation: `age-keygen -o staging-backup-age-identity.txt`, paste the `Public key:` line into the VM's `.env`, and keep the identity file only in the password manager and one offline copy (never on the VM, never in the repo). Do not regenerate it: every existing backup and WAL segment is encrypted to the old recipient, so losing or replacing the identity makes them unreadable.
- `LESSON_ASSET_ORIGIN` is a public https origin when `DUOLIFY_ENV` is `staging` or `production`. The lesson iframe's CSP only lets a slide load from this origin and it is stored in the lesson HTML, so `http://localhost:9000` breaks every lesson. With R2, attach a custom domain to the application bucket (for example `assets.staging.duolify.online`, proxied by Cloudflare) and use `https://<that host>`. Set it before lessons are generated; lessons stored under another origin keep the old one.
- `PUBLIC_ORIGIN` is the https origin of the web client without a path, and `SITE_ADDRESS` names the served hosts (not `:80`). `PUBLIC_ORIGIN` is also the default `API_ALLOWED_ORIGINS`; set `API_ALLOWED_ORIGINS` (comma-separated) to allow more, and set `CLERK_*` for Clerk web sign-in (cookie mode refuses to start without an allowed origin).
- No `GENERATED`, `CHANGE_ME` or `ACCOUNT_ID` placeholder is left in `.env`.
- The memory limits add up to at most 3.5 GB and the database pools fit under `max_connections` (see [hardening.md](hardening.md#8-sizing-cx23-2-vcpu-4-gb-40-gb)).

## Where secrets live (names only)

- Owner workstation: `~/.config/duolify/secrets.env` (provider keys), `~/.config/duolify/deploy_staging_ed25519` (deploy key), `staging-origin.key` / `staging-origin.pem` (origin certificate), and `staging-backup-age-identity.txt` (the backup age private key, which is not on the VM). Copy the age identity to the password manager.
- VM: `infra/deploy/.env` and `infra/deploy/site.d/origin.key`.
- GitHub: the `staging` environment secrets above. The deploy job pulls the private GHCR images with its own `GITHUB_TOKEN` (`packages: read`), which expires when the job ends.

## Deploy

Every successful `pr-ready` push run on `main` triggers `deploy-vm` for `staging` automatically. To redeploy a given commit by hand:

```sh
gh run list --workflow pr-ready --branch main --event push --status success --limit 5   # pick the run id
gh workflow run deploy-vm.yml -f environment=staging -f publish_run_id=<run id>
```

The job runs `infra:find-vm` (a Hetzner lookup of `duolify-staging`), then `infra:deploy-vm`, then `infra/deploy/deploy.sh` on the VM. `deploy.sh` logs in to ghcr.io with the job token on stdin, pulls the five digests and logs out. It then runs `/api migrate` and restarts the Engine, the API instances one at a time, the worker and the proxy, then runs `selfhost.sh smoke`.

## How the smoke check connects

`selfhost.sh smoke` tests the origin, not Cloudflare. It connects to `BIND_ADDRESS` (the VM's public IP on staging; loopback when it is empty or `0.0.0.0`) on `HTTPS_PORT`, with the first `SITE_ADDRESS` name as SNI and Host (`curl --resolve`) and `site.d/origin-ca.pem` as the trusted CA. It first waits up to `SMOKE_WAIT_SECONDS` (60) for the listener, because the proxy container is healthy (its healthcheck uses the unpublished port 8081) before Caddy answers on 443. Every other `SITE_ADDRESS` name is then checked on the same listener. A last check goes through the public name (Cloudflare) and only warns (`SMOKE_PUBLIC=0` skips it).

The smoke test used to connect to `127.0.0.1`, where nothing listens when `BIND_ADDRESS` is the public IP, so every deploy ended with `curl: (7) Failed to connect ... after 0 ms` and rolled back. Test: `infra/deploy/test-smoke.sh`.

## Read a failed deploy

Open the `deploy-vm` run, step "Deploy over SSH". The last `==>` lines say the phase: a failure before `rolling restart` leaves the old containers running; a smoke failure means the new generation had been started, and `deploy.sh` then restarts engine, API, worker and proxy on the previous image references (`deploy: rolled back to the previous image references`). `deploy: ROLLBACK FAILED` means a mix: compare `sudo docker inspect -f '{{.Config.Image}}' duolify-selfhost-<svc>-1` with the `*_IMAGE=` lines in `.env`.

## Roll back

Run the workflow again with the `publish_run_id` of the previous good `pr-ready` run. Migrations stay applied (they are expand/contract, see [hardening.md](hardening.md#7-deploys)). A database restore is only for data loss.

## Check it

```sh
curl -fsS https://staging.duolify.online/v1/features
curl -fsS https://staging-api.duolify.online/v1/features
ssh admin@<ip> 'cd /opt/duolify && sudo -u deploy env SELFHOST_OVERLAY=infra/deploy/compose.prod.yml bash infra/deploy/selfhost.sh smoke'
```

## Verify staging

```sh
curl -fsS https://staging-api.duolify.online/v1/features
curl -fsS https://staging-api.duolify.online/v1/pricing
curl -sS --max-time 15 https://staging-api.duolify.online/v1/stream-check          # SSE completes through Cloudflare
ssh admin@<ip> 'sudo docker ps --format "{{.Names}} {{.Image}} {{.Status}}"'        # images match .env digests
ssh admin@<ip> 'sudo docker logs --since 1h duolify-selfhost-worker-1 2>&1 | grep -ci x509'   # expect 0
ssh admin@<ip> 'sudo docker exec duolify-selfhost-postgres-1 psql -U postgres -c "select archived_count, last_archived_time, failed_count from pg_stat_archiver"'
```

## After you change `.env` settings of a data service

`deploy.sh` never recreates Postgres, Redis, walship, Prometheus or node-exporter, so a changed bucket name, memory limit or Postgres setting in `.env` does not reach a running container. On staging walship kept shipping WAL to the old shared `duolify-backups` bucket (and `pg_stat_archiver` looked healthy) until the documented recreate in [hardening.md](hardening.md#8-sizing-cx23-2-vcpu-4-gb-40-gb) was run (Postgres was down for about 4 s; the API served throughout). After any such change, check what runs:

```sh
sudo docker inspect duolify-selfhost-postgres-1 --format '{{.HostConfig.Memory}}'      # POSTGRES_MEMORY
sudo docker exec duolify-selfhost-walship-1 sh -c 'echo $BACKUP_S3_BUCKET $S3_BUCKET'  # staging buckets
```

Then force a segment and a base backup and list the bucket (`pg_switch_wal()`, `/walship.sh base` in the walship container; `aws s3 ls s3://duolify-staging-backups/wal/` with the backup key in a throwaway `amazon/aws-cli` container). The newest base decrypts with the owner age identity (`age -d -i staging-backup-age-identity.txt | tar -tz`).

## Known gaps

- Staging and future production share the R2 buckets `duolify-assets` and `duolify-backups`, and one R2 key serves both roles. Before production writes to these buckets, give staging its own buckets (or production its own) and split the keys by role. Otherwise both stacks write `wal/` and `base/` into the same prefixes.
- SSH is open to every address. Restricting it needs fixed runner addresses or a self-hosted runner.
- Authenticated Origin Pulls are not enabled. The firewall limits 80/443 to Cloudflare's ranges, so another Cloudflare zone can still reach the origin.
- Prometheus has no notifier.
