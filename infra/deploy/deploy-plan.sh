#!/usr/bin/env bash
# Static validation of the production overlay; starts nothing. Usage: deploy-plan.sh [DIR]
# DIR (default: this directory) holds compose.yml, compose.prod.yml, env (default .env.prod.example)
# and alerts.yml (default ../observability/alerts.yml). Exits 1 listing every violated rule.
#
# Limits (this is a static lint of the rendered Compose config, not a security boundary):
#  - Port rules read `ports` after Compose normalises short and long syntax; they do not see
#    `expose`, host networking via other means, or anything started outside this project.
#  - The pooler rule matches service name/image against known pooler products and requires the
#    DATABASE_URL of api/worker/migrate to target postgres:5432; a pooler behind another name,
#    or a different DSN key, is not detected.
#  - Location rules compare PRIMARY_LOCATION/BACKUP_LOCATION and the endpoint host prefix as strings,
#    and require different app and backup buckets; they cannot know where a custom endpoint really is.
#  - Environment rules read the env file only (not variables exported by the caller). The checked-in
#    template (.env.prod.example, used when DIR has no `env`) may keep its placeholders; a real env file
#    may not. deploy.sh runs this against infra/deploy/.env before it pulls or migrates anything.
#  - The memory rule sums the rendered limits of the services that run on the VM (no profile), times
#    replicas, against DEPLOY_MEMORY_BUDGET_MB (default 3584 MiB of a 4 GB VM); it cannot see real use.
#  - Alert rules are checked by name and by the absent() expression text, not by behaviour;
#    infra/observability/alerts.test.yml (promtool) covers behaviour.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
dir=${1:-$here}
env_file=${DEPLOY_PLAN_ENV:-$dir/env}
template=0
if [[ ! -f $env_file ]]; then env_file=$dir/.env.prod.example; template=1; fi
alerts=$dir/alerts.yml
[[ -f $alerts ]] || alerts=$here/../observability/alerts.yml

problems=()
bad() { problems+=("$*"); }
setting() { sed -n "s/^$1=//p" "$env_file" | tail -n 1; }

# 3. Backup environment is complete, and the backup endpoint is outside the primary location.
for key in PRIMARY_LOCATION BACKUP_LOCATION S3_ENDPOINT BACKUP_S3_ENDPOINT BACKUP_S3_REGION BACKUP_S3_BUCKET BACKUP_S3_ACCESS_KEY_ID BACKUP_S3_SECRET_ACCESS_KEY BACKUP_AGE_RECIPIENT; do
  if [[ -z $(setting "$key") ]]; then bad "missing $key in $(basename "$env_file")"; fi
done
primary=$(setting PRIMARY_LOCATION)
backup=$(setting BACKUP_LOCATION)
endpoint_host=$(setting BACKUP_S3_ENDPOINT | sed -e 's#^[a-z]*://##' -e 's#[/:].*##')
if [[ -n $primary && $backup == "$primary" ]]; then bad "BACKUP_LOCATION ($backup) equals PRIMARY_LOCATION; backups must leave the primary site"; fi
if [[ -n $primary && $endpoint_host == "$primary".* ]]; then bad "BACKUP_S3_ENDPOINT ($endpoint_host) is in the primary location $primary"; fi
if [[ -n $(setting S3_BUCKET) && $(setting S3_BUCKET) == "$(setting BACKUP_S3_BUCKET)" && $(setting S3_ENDPOINT) == "$(setting BACKUP_S3_ENDPOINT)" ]]; then bad "BACKUP_S3_BUCKET is the application bucket (S3_BUCKET) on the same endpoint; backups need their own bucket"; fi
for key in S3_REGION BACKUP_S3_REGION; do
  if [[ -z $(setting "$key") ]]; then bad "missing $key (use auto for Cloudflare R2)"; fi
done

# 3b. Settings the API or Caddy would only reject after images were pulled and the database migrated.
# The API refuses to start when the backup writer reuses the application key (backup.Load).
app_key=$(setting S3_ACCESS_KEY_ID); app_key=${app_key:-duolify-app}
backup_key=$(setting BACKUP_S3_ACCESS_KEY_ID); backup_key=${backup_key:-duolify-backup-writer}
if [[ $app_key == "$backup_key" ]]; then bad "BACKUP_S3_ACCESS_KEY_ID equals S3_ACCESS_KEY_ID; the API refuses to start unless the backup writer uses separate credentials (create a second token scoped to the backup bucket)"; fi

# deploy.sh never generates the backup key pair: the public age recipient must already be here.
# age recipients are bech32: "age1" plus 58 characters from the bech32 alphabet.
recipient=$(setting BACKUP_AGE_RECIPIENT)
if [[ -n $recipient && ! ($template == 1 && $recipient == GENERATED) && ! $recipient =~ ^age1[02-9ac-hj-np-z]{58}$ ]]; then
  bad "BACKUP_AGE_RECIPIENT is not an age recipient (age1...); generate the key pair once with age-keygen, keep the identity off the VM and put the public key in the env file (docs/infra/staging.md)"
fi

runtime_env=$(setting DUOLIFY_ENV); runtime_env=${runtime_env:-development}
if [[ $runtime_env == staging || $runtime_env == production ]]; then
  # The lesson iframe CSP allows network only to this origin, and it is baked into stored lesson HTML.
  asset_origin=$(setting LESSON_ASSET_ORIGIN); asset_origin=${asset_origin:-http://localhost:9000}
  if [[ ! $asset_origin =~ ^https://[^/?#@\ ]+$ || $asset_origin =~ ^https://(localhost|127(\.[0-9]+)*|\[::1\]|0\.0\.0\.0)([:/]|$) ]]; then
    bad "LESSON_ASSET_ORIGIN '$asset_origin' must be a public https origin (scheme://host, no path) on $runtime_env; the lesson iframe CSP only allows that origin, so localhost breaks every slide (R2 layout: the public custom domain of the application bucket, e.g. https://assets.duolify.online)"
  fi
  public_origin=$(setting PUBLIC_ORIGIN)
  if [[ ! $public_origin =~ ^https://[^/?#@\ ]+$ ]]; then bad "PUBLIC_ORIGIN '$public_origin' must be the https origin of the web client (scheme://host, no path) on $runtime_env; it is the API's CORS and cookie-mode allowlist"; fi
  site=$(setting SITE_ADDRESS)
  if [[ -z $site || $site == :* || $site == http://* ]]; then bad "SITE_ADDRESS '${site}' must name the served host(s) with automatic HTTPS on $runtime_env (not empty, ':80' or http://)"; fi
fi

# A filled environment file keeps no placeholder from the template.
if ((template == 0)); then
  placeholders=$(grep -nE '^[A-Z0-9_]+=(GENERATED|CHANGE_ME[A-Z_]*)$|^[A-Z0-9_]+=.*ACCOUNT_ID' "$env_file" | cut -d: -f1 | tr '\n' ' ' || true)
  if [[ -n $placeholders ]]; then bad "template placeholders left in $(basename "$env_file") (GENERATED, CHANGE_ME, ACCOUNT_ID) on lines: $placeholders"; fi
fi

config=$(docker compose --project-directory "$dir" --file "$dir/compose.yml" --file "$dir/compose.prod.yml" \
  --env-file "$env_file" --profile '*' config --format json) || { bad "compose config failed (a required variable is unset or the files are invalid; see above)"; config=''; }
if [[ -z $config ]]; then # nothing below can be checked without the rendered config
  printf 'deploy-plan: %s\n' "${problems[@]}" >&2
  exit 1
fi

# 0. No service may share the host network namespace (it would bypass every port rule below).
hostnet=$(jq -r '.services | to_entries[] | select(.value.network_mode == "host") | .key' <<<"$config")
if [[ -n $hostnet ]]; then bad "services use network_mode: host (bypasses port isolation): $(tr '\n' ' ' <<<"$hostnet")"; fi

# 1. The Engine is never public: no published port on it, and only the proxy publishes any.
if [[ $(jq '.services.engine.ports // [] | length' <<<"$config") != 0 ]]; then bad "ENGINE publishes a host port; the engine must stay private"; fi
exposed=$(jq -r '.services | to_entries[] | select(.key != "proxy" and (.value.ports // [] | length) > 0) | .key' <<<"$config")
if [[ -n $exposed ]]; then bad "services publish host ports besides the proxy: $(tr '\n' ' ' <<<"$exposed")"; fi

# 2. Direct Postgres connections: no pooler service, and the DB URL goes to postgres:5432.
if jq -e '.services | to_entries[] | select(.key + (.value.image // "") | test("pgbouncer|pgcat|odyssey|supavisor|pgpool"; "i"))' <<<"$config" >/dev/null; then
  bad "a transaction pooler service is defined; the API and worker need direct Postgres sessions (advisory locks, River)"
fi
for svc in api worker migrate; do
  url=$(jq -r ".services.$svc.environment.DATABASE_URL // empty" <<<"$config")
  if [[ $url != *@postgres:5432/* ]]; then bad "pooler or non-direct DATABASE_URL for $svc: must target postgres:5432 directly"; fi
done

# 4. WAL archiving and the DR copy are wired.
if ! jq -e '.services.postgres.command | join(" ") | test("archive_mode=on") and test("archive_command=")' <<<"$config" >/dev/null; then bad "postgres has no WAL archiving (archive_mode/archive_command)"; fi
jobs=$(jq -r '.services.walship.environment.WALSHIP_JOBS // ""' <<<"$config")
for job in ship base dr-copy; do
  if [[ ,$jobs, != *",$job,"* ]]; then bad "walship job $job missing from WALSHIP_JOBS ('$jobs'); the nightly DR copy needs ship,base,dr-copy"; fi
done

# 5. Alerts cover disk, WAL, backup and DR age (and their absence), bucket usage.
for name in DuolifyDiskLow DuolifyWALArchiveFailing DuolifyWALSpoolBacklog DuolifyBackupStale DuolifyBucketUsageHigh DuolifyDRCopyStale DuolifyBaseBackupMissing DuolifyNoRecentBaseInBucket DuolifyDRCopyMissing DuolifyWalshipMetricsMissing; do
  if ! grep -q "alert: $name\$" "$alerts"; then bad "alert $name missing from $(basename "$alerts")"; fi
done

# A never-written metric must fire too: time() - x is empty when x is absent.
for metric in duolify_base_last_success_timestamp_seconds duolify_dr_last_success_timestamp_seconds duolify_wal_spool_oldest_age_seconds; do
  if ! grep -q "absent($metric)" "$alerts"; then bad "no absent($metric) rule in $(basename "$alerts"); a never-written metric would not alert"; fi
done
# There is no replica in this stack: a replication-lag alert could never fire and only hides gaps.
if grep -q 'alert: DuolifyReplicationLag' "$alerts"; then bad "DuolifyReplicationLag alert present (DuolifyReplicationLag); this stack has no replica, remove it"; fi

# 6. The 4 GB VM: limits of the services that run there (no profile) stay under the budget.
budget=${DEPLOY_MEMORY_BUDGET_MB:-3584}
total=$(jq '[.services | to_entries[] | select(.value.profiles == null) | (.value.deploy.resources.limits.memory // "0" | tonumber) * (.value.deploy.replicas // 1)] | add / 1048576 | floor' <<<"$config")
if ((total > budget)); then bad "memory limits of the VM services add up to ${total} MiB, over the ${budget} MiB budget (a 4 GB VM needs room for the OS and page cache); lower POSTGRES_MEMORY or the service limits"; fi

# 7. Connections: every pool of every process, with one extra API replica during a rolling restart and
# 8 for the reserved superuser slots, walship and an operator, must fit under max_connections.
max_conn=$(jq -r '.services.postgres.command | join(" ")' <<<"$config" | sed -n 's/.*max_connections=\([0-9]*\).*/\1/p')
api_pool=$(jq -r '.services.api.environment.DB_POOL_MAX_CONNS // "0"' <<<"$config")
worker_pool=$(jq -r '.services.worker.environment.DB_POOL_MAX_CONNS // "0"' <<<"$config")
replicas=$(jq -r '.services.api.deploy.replicas // 1' <<<"$config")
if [[ -z $max_conn ]]; then bad "postgres sets no max_connections; size it for the API and worker pools"
elif [[ $api_pool == 0 || $worker_pool == 0 ]]; then bad "api or worker has no DB_POOL_MAX_CONNS; the pgx default (max(4, CPUs)) is not sized for this box"
else
  peak=$(((replicas + 1) * api_pool + worker_pool + 8))
  if ((peak > max_conn)); then bad "database pools need up to $peak connections ((${replicas}+1) API x $api_pool + worker $worker_pool + 8) but postgres max_connections is $max_conn"; fi
fi

if ((${#problems[@]} > 0)); then
  printf 'deploy-plan: %s\n' "${problems[@]}" >&2
  exit 1
fi
echo "deploy-plan: ok"
