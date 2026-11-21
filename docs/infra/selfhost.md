# Self-hosted stack

One command runs the whole production-shaped stack on any Linux machine with Docker.
It is the base for the Hetzner VM deployment in [ADR 0014](../adr/0014-infrastructure-cost-migration.md).

| Service | Role | Published |
|---|---|---|
| `proxy` | Caddy: web client on `/`, API on `/v1` (SSE unbuffered), HTTPS when `SITE_ADDRESS` is a domain | `HTTP_PORT`, `HTTPS_PORT` |
| `api`, `worker` | Go API and River jobs (encrypted nightly backup at 02:00 UTC and on start) | no |
| `engine` | Rust engine, mock providers by default | no |
| `postgres`, `redis`, `objectstore` | Postgres 18.6, Redis 8.4, RustFS with versioned `duolify` and `duolify-backups` buckets | no |
| `s3-init`, `backup-init`, `migrate` | One-shot: buckets, scoped RustFS identities, Goose + River migrations | no |

Files: `infra/deploy/compose.yml` (self-contained; it does not extend the dev `docker-compose.yml`),
`infra/deploy/Caddyfile`, `infra/deploy/.env.example`, `infra/deploy/selfhost.sh`.
Data lives in the named volumes of the Compose project `duolify-selfhost`.

## Prerequisites

Docker Engine with Compose v2.24+, `bash`, `curl`, and a clone of this repository.
`mise` is optional: every `mise run infra:selfhost-*` task is a thin wrapper around `bash infra/deploy/selfhost.sh <command>`.
Building images needs about 10 GB of free disk; pulling them needs about 2 GB.

## First run

```sh
bash infra/deploy/selfhost.sh init      # creates infra/deploy/.env with random secrets
$EDITOR infra/deploy/.env               # optional: ports, domain, providers
mise run infra:selfhost-up              # build (IMAGE_TAG=local) or pull, start, wait for health
mise run infra:selfhost-smoke
```

Open `PUBLIC_ORIGIN` (default `http://localhost`). The stack needs no external credentials:
`DUOLIFY_ENV=development` writes sign-in codes to the API log (`mise run infra:selfhost-logs api`), the engine uses mock providers, and analytics are off.
The first `up` also writes the backup age identity to `infra/deploy/secrets/backup-age-identity.txt`.

Settings to review in `infra/deploy/.env`:

- **Local test on busy ports:** `BIND_ADDRESS=127.0.0.1`, `HTTP_PORT=8088`, `PUBLIC_ORIGIN=http://localhost:8088`.
- **Public server:** `SITE_ADDRESS=learn.example.com`, `PUBLIC_ORIGIN=https://learn.example.com`, default ports 80/443. Point DNS at the host first, and Caddy obtains the certificate.
- **Real users:** `DUOLIFY_ENV=production`, `RESEND_API_KEY` and `AUTH_MAIL_FROM`.
- **Live AI:** `ENGINE_PROVIDER_MODE=live` with `OPENROUTER_API_KEY` or `ANTHROPIC_API_KEY`.

`PUBLIC_ORIGIN` must match the browser URL exactly. The API rejects other origins.

## Day-to-day

| Task | Command |
|---|---|
| Start / upgrade | `mise run infra:selfhost-up` |
| Stop (keeps data) | `mise run infra:selfhost-down` |
| Logs | `mise run infra:selfhost-logs [service...]` |
| Health check | `mise run infra:selfhost-smoke` |
| Back up now | `mise run infra:selfhost-backup` |
| Restore drill | `mise run infra:selfhost-restore-drill` |
| Anything else | `bash infra/deploy/selfhost.sh compose <args>` (for example `ps` or `exec postgres psql -U duolify`) |

Always go through `selfhost.sh`. It pins the project, the compose file and the env file, and it hides variables from your shell and the dev `.env` that mise loads.

**Upgrade:** `git pull` and run `mise run infra:selfhost-up`. This rebuilds the images, runs `migrate` and recreates any changed services.
To use CI images instead, set `IMAGE_REGISTRY=ghcr.io/datamonsterr/duolify` and `IMAGE_TAG=sha-<main commit>`. CI publishes `api`, `worker`, `backup-init`, `engine` and `web` on every push to `main`.
If the packages are private, run `docker login ghcr.io` first.

## Backup and restore

- The worker writes `duolify-backups/YYYY-MM-DD/schema-<n>.dump.age` (age-encrypted `pg_dump`) every night and on start.
  It also writes encrypted copies of lesson assets. Both buckets are versioned. See [infra/backup/README.md](../../infra/backup/README.md).
- `infra:selfhost-backup` makes sure today's dump exists. If it is missing, the command restarts the worker and waits for it.
- `infra:selfhost-restore-drill` downloads the newest dump with the read-only restorer identity and decrypts it with the age identity.
  It restores the dump into a throwaway Postgres container with no network, then checks the migrations and the credit ledger.
  The plaintext exists only in a pipe and in that container.
- With the plain stack, backups are on the same disk. The production overlay (below) ships them, the WAL archive and the DR copy off-site.
  Keep the age identity separate from the backups.
- Expiry of old backups (`cmd/backup-expire`) does not run here yet. Daily dumps accumulate until you delete them.

**Restoring into a fresh stack:** run `up` on the new machine with the copied `.env`. Then load a decrypted dump:

```sh
bash infra/deploy/selfhost.sh compose stop api worker
age -d -i backup-age-identity.txt dump.age | bash infra/deploy/selfhost.sh compose exec -T postgres pg_restore --clean --if-exists -U duolify -d duolify
bash infra/deploy/selfhost.sh compose start api worker
```

## Production overlay (hel1)

`infra/deploy/compose.prod.yml` stacks on `compose.yml` for the Hetzner VM ([ADR 0014](../adr/0014-infrastructure-cost-migration.md)). Use it with `SELFHOST_OVERLAY=infra/deploy/compose.prod.yml` (the CI tasks and `deploy.sh` set it).
Seed the env file with `SELFHOST_ENV_TEMPLATE=infra/deploy/.env.prod.example bash infra/deploy/selfhost.sh init`.

- **Services:** API x2, worker, Engine (private only), Redis, Postgres 18, `walship`, node-exporter and Prometheus. No pooler: the API and worker connect to Postgres directly.
- **Object storage from env:** `S3_*` is the application bucket (Hetzner Object Storage `hel1`). `BACKUP_S3_*` is the backup bucket in `BACKUP_LOCATION` (`fsn1` or `nbg1`, never the primary). RustFS and the scoped-identity initializers only run with `COMPOSE_PROFILES=local-s3` (CI, local tests).
- **WAL archiving (7-day PITR):** Postgres copies each finished WAL segment to a spool volume (`archive_mode=on`, `archive_command`, `archive_timeout=300`). `walship` gzips and age-encrypts it and uploads it to `wal/` in the backup bucket, then removes it. It also uploads an encrypted `pg_basebackup` to `base/<UTC time>/` nightly at 02:30 UTC. Set a bucket lifecycle rule that expires `wal/` and `base/` after 7 days; the scoped writer cannot delete.
- **Nightly DR copy:** `walship dr-copy` copies new or changed objects of the application bucket to `dr/lesson-assets/` in the versioned backup bucket. It never deletes, so every version survives source overwrites and deletes.
- **PITR drill:** `bash infra/deploy/selfhost.sh pitr-drill '2026-11-21 10:00:00+00'` restores the newest base backup before that time plus the WAL archive into a throwaway Postgres and prints the result. It uses the age identity from `infra/deploy/secrets/`.
- **Alerts** (`infra/observability/alerts.yml`, evaluated by the overlay's Prometheus): `DuolifyDiskLow`, `DuolifyWALArchiveFailing`, `DuolifyWALSpoolBacklog` (WAL lag), `DuolifyBackupStale`, `DuolifyReplicationLag`, `DuolifyBucketUsageHigh`, `DuolifyDRCopyStale`. Prometheus has no notifier yet: add Alertmanager (or Grafana contact points) before relying on them.
- **Checks:** `mise run infra:deploy-plan` rejects a public Engine port, a pooler, a missing backup env, a missing DR job and a backup endpoint in the primary location. `infra:overlay-e2e`, `-pitr`, `-redis` and `-dr` run the stack tests on local RustFS.
- **Deploy:** `.github/workflows/deploy-vm.yml` runs after `pr-ready` (which runs `ci-ok` and publishes the GHCR images) and calls `mise run infra:deploy-vm`: it copies `infra/` to the VM, then `deploy.sh sha-<commit>` pulls the images (logging their digests), runs `/api migrate`, and restarts the Engine, each API instance one at a time, the worker and the proxy. The job skips itself unless the `staging` or `production` environment has `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_SSH_KEY` and `DEPLOY_KNOWN_HOSTS`.
- **Hardening:** see [hardening.md](hardening.md).

## Move to another machine

1. On the old machine, run `mise run infra:selfhost-down`. Copy `infra/deploy/.env` and `infra/deploy/secrets/` over a secure channel.
2. Copy the data. Use either option:
   - **Copy the volumes.** On both machines, the volumes are `duolify-selfhost_postgres-data`, `_objectstore-data` and `_caddy-data`.
     For example: `docker run --rm -v duolify-selfhost_postgres-data:/v -v "$PWD":/b alpine tar czf /b/pg.tgz -C /v .`, and the reverse on the new host.
   - **Restore from a backup.** Follow the steps in the previous section. Postgres is the source of truth, and Redis rebuilds itself.
3. On the new machine, clone the repository, put the files back, and run `mise run infra:selfhost-up` and then `mise run infra:selfhost-smoke`.
   If the domain changed, update `SITE_ADDRESS` and `PUBLIC_ORIGIN`. Do not change `LESSON_ASSET_ORIGIN`.

The secrets in `.env` must match the volumes they were created with. The RustFS identities are refreshed on every start, but the Postgres password is fixed when its volume is created.

## Troubleshooting

- **`missing infra/deploy/.env`:** run `bash infra/deploy/selfhost.sh init`.
- **Port already in use:** change `HTTP_PORT` and `HTTPS_PORT`, or set `BIND_ADDRESS` to a single interface.
- **`origin not allowed` (403) in the browser:** set `PUBLIC_ORIGIN` to the exact URL in the address bar, then run `up`.
- **A service is unhealthy:** run `bash infra/deploy/selfhost.sh compose ps` and then `mise run infra:selfhost-logs <service>`.
  The worker fails fast on a missing `BACKUP_AGE_RECIPIENT`.
- **HTTPS certificate not issued:** DNS must resolve to this host, and ports 80 and 443 must be reachable from the internet.
  Check the logs with `mise run infra:selfhost-logs proxy`.
- **Reset everything (deletes all data):** `bash infra/deploy/selfhost.sh compose down --volumes`.
