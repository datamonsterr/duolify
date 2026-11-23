# Fresh-deploy and disaster-recovery runbook

Status: replaces the planned Railway cutover runbook. ADR 0015 (D1) decided to deploy fresh on Hetzner with Cloudflare R2 and not to migrate Railway data, so there is no plan A or plan B, no replication, no switchover gate and no old store. This runbook covers two cases that remain: bringing up a new environment (staging today, production later), and recovering one after losing the VM or the database. Stack and settings: [selfhost.md](selfhost.md), [staging.md](staging.md), [hardening.md](hardening.md). Backup tooling: [../../infra/backup/README.md](../../infra/backup/README.md).

Roles: steps marked (owner) need Hetzner, Cloudflare, R2 or password-manager access. Never put secrets in the repository or in tickets; names only.

## 1. Preflight (owner)

1. Hetzner: server `duolify-<env>` (type per [staging.md](staging.md), 4 GB RAM minimum) with the firewall: 80 and 443 only from Cloudflare ranges, 22 by key.
2. Cloudflare: zone `duolify.online`, proxied A records for the web host and API host, SSL mode Full (strict) with an Origin CA certificate loaded in `infra/deploy/site.d/`.
3. R2: buckets `duolify-assets` and `duolify-backups` (different buckets, separate keys for the app, backup writer and restorer where R2 allows). Custom domain on the asset bucket for `LESSON_ASSET_ORIGIN`. Apply the lifecycle: `mise run infra:backup-lifecycle`, then check `mise run infra:backup-preflight`.
4. Secrets: GitHub environment `<env>` holds `DEPLOY_USER`, `DEPLOY_SSH_KEY` and `DEPLOY_KNOWN_HOSTS`; repo secret `HCLOUD_TOKEN`. The age backup identity exists only in the password manager and one offline copy.
5. VM `.env` is complete (names in [staging.md](staging.md#runtime-settings-names-only)). `mise run infra:deploy-plan` passes against it; `deploy.sh` runs it too.
6. Record the PostHog baseline for the parity check (section 4): note the last seven days of hourly event counts for the event families below.

## 2. Fresh deploy

1. Trigger the deploy of a commit that passed `ci-ok`: automatic after `pr-ready` on `main` for staging, otherwise `gh workflow run deploy-vm.yml -f environment=<env> -f publish_run_id=<run id>`.
2. The job runs `infra:find-vm`, `infra:deploy-vm` and `deploy.sh`: it pulls the pinned image digests, runs `/api migrate`, restarts the Engine, the API instances one at a time, the worker and the proxy, then `selfhost.sh smoke`. A failed smoke rolls the generation back; read the log as described in [staging.md](staging.md#read-a-failed-deploy).
3. Verify through the public host names (Cloudflare, not the origin):
   - `GET /readyz` is ready, and the web client loads with the security headers ([selfhost.md](selfhost.md#security-headers)).
   - Sign in with a test learner, play a lesson and open a Tutor chat; the SSE stream must show heartbeats within 25 s and survive 10 minutes without a 524 (`node infra/load/sse-stream.mjs`).
   - `mise run api:posthog-verify` (needs the PostHog variables) confirms ingestion.
   - The first worker backup appears: `YYYY-MM-DD/schema-<n>.dump.age` in `duolify-backups`, and after 03:00 UTC a base backup under `base/`.
4. Run the evals before real learners: `mise run engine:eval` and the live sample eval (OpenRouter route from `engine/config/models.toml`). A score below the CI thresholds stops the launch.
5. Rollback of a bad release: redeploy the previous good commit with step 1; migrations are forward-only, so a release that ships a migration needs a forward fix or a restore (section 5).

## 3. Drills (staging, before production)

- Restore drill: `mise run infra:selfhost-restore-drill` (or `infra:restore-drill-scheduled` with owner credentials) restores the newest dump into a throwaway Postgres and checks migrations and the credit ledger.
- PITR drill: `bash infra/deploy/selfhost.sh pitr-drill '<UTC timestamp>'`; the result must show the data of that moment.
- Load baseline: `mise run infra:load-baseline`, and the same k6 plus SSE scripts from a Southeast Asian vantage point against the public host; record both in [baseline.md](baseline.md).

## 4. PostHog parity check

No event is added or renamed by deploying or restoring. For 24 hours after a traffic-changing step (first production deploy, DNS switch, restore), compare hourly counts with the same hours of the previous week, for product events and `$ai_generation`:

- Within plus or minus 20 percent: the step stands.
- A drop below the tolerance: roll the step back (previous release, or the previous DNS target), record the cause, and repeat.
- During a first production launch there is no previous week; use staging's hourly profile scaled by active learners, and extend the watch to 72 hours.

Query in PostHog (HogQL), per hour: `select toStartOfHour(timestamp) h, event, count() from events where timestamp > now() - interval 24 hour group by h, event order by h`. Run it again with the 7-day offset and compare. Learner text is never in these events; the check needs counts only.

## 5. Disaster recovery

Targets: lost VM or disk, corrupted database, lost bucket object. The data sources are Postgres (source of truth, backed up), R2 objects (assets, DR copy) and Redis (rebuilds from Postgres). Keep the age identity from the password manager at hand.

### 5.1 Lost VM, database volume intact in the backups

1. Provision a VM as in section 1 (same names, same firewall, DNS re-points to the new IP).
2. Copy `infra/deploy/.env` and the secrets from the password manager, then deploy the last good commit (section 2). The deploy creates an empty database.
3. Stop the API and worker, restore Postgres from the newest base backup plus WAL (`selfhost.sh pitr-drill` shows the procedure on a throwaway instance; for the real restore use the same steps against the live Postgres container), or from the newest nightly dump:

   ```sh
   bash infra/deploy/selfhost.sh compose stop api worker
   age -d -i backup-age-identity.txt dump.age | bash infra/deploy/selfhost.sh compose exec -T postgres pg_restore --clean --if-exists -U duolify -d duolify
   bash infra/deploy/selfhost.sh compose start api worker
   ```
4. Account deletions replay automatically after a restore from the `account-deletions/` ledger in the backup bucket (never expired); confirm the worker log shows the replay finished before opening traffic.
5. Smoke and verify as in section 2, then run the PostHog parity check (section 4).

### 5.2 Lost or overwritten asset objects

R2 has no versioning. Restore from the nightly DR copy `dr/lesson-assets/` in the backup bucket (`walship dr-copy` never deletes). Re-publish an object under its original key with the recovery job described in [../../infra/backup/README.md](../../infra/backup/README.md) (a job id is mandatory). Objects that no copy holds are regenerated through a tracked job, never silently.

### 5.3 Whole-region or provider loss

Recreate the VM in another Hetzner location (`PRIMARY_LOCATION` changes; `BACKUP_LOCATION` must still differ), reuse the R2 buckets, and follow 5.1. Cloudflare R2 and Hetzner are independent providers, so one loss does not take the backups with it.

## 6. After the event

Record what happened, the recovery time and the data gap (PITR target versus last WAL) in the operations log. Remove temporary access you created. Open an issue for anything that deviated from this runbook and fix the runbook in the same PR.
