#!/usr/bin/env bash
# Static validation of the production overlay; starts nothing. Usage: deploy-plan.sh [DIR]
# DIR (default: this directory) holds compose.yml, compose.prod.yml, env (default .env.prod.example)
# and alerts.yml (default ../observability/alerts.yml). Exits 1 listing every violated rule.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
dir=${1:-$here}
env_file=$dir/env
[[ -f $env_file ]] || env_file=$dir/.env.prod.example
alerts=$dir/alerts.yml
[[ -f $alerts ]] || alerts=$here/../observability/alerts.yml

problems=()
bad() { problems+=("$*"); }
setting() { sed -n "s/^$1=//p" "$env_file" | tail -n 1; }

config=$(docker compose --project-directory "$dir" --file "$dir/compose.yml" --file "$dir/compose.prod.yml" \
  --env-file "$env_file" --profile '*' config --format json) || { echo "deploy-plan: compose config failed" >&2; exit 1; }

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

# 3. Backup environment is complete, and the backup endpoint is outside the primary location.
for key in PRIMARY_LOCATION BACKUP_LOCATION S3_ENDPOINT BACKUP_S3_ENDPOINT BACKUP_S3_REGION BACKUP_S3_BUCKET BACKUP_S3_ACCESS_KEY_ID BACKUP_S3_SECRET_ACCESS_KEY BACKUP_AGE_RECIPIENT; do
  if [[ -z $(setting "$key") ]]; then bad "missing $key in $(basename "$env_file")"; fi
done
primary=$(setting PRIMARY_LOCATION)
backup=$(setting BACKUP_LOCATION)
endpoint_host=$(setting BACKUP_S3_ENDPOINT | sed -e 's#^[a-z]*://##' -e 's#[/:].*##')
if [[ -n $primary && $backup == "$primary" ]]; then bad "BACKUP_LOCATION ($backup) equals PRIMARY_LOCATION; backups must leave the primary site"; fi
if [[ -n $primary && $endpoint_host == "$primary".* ]]; then bad "BACKUP_S3_ENDPOINT ($endpoint_host) is in the primary location $primary"; fi
if [[ -n $backup && -n $endpoint_host && $endpoint_host == *.your-objectstorage.com && $endpoint_host != "$backup".* ]]; then bad "BACKUP_S3_ENDPOINT ($endpoint_host) is not in BACKUP_LOCATION ($backup)"; fi

# 4. WAL archiving and the DR copy are wired.
if ! jq -e '.services.postgres.command | join(" ") | test("archive_mode=on") and test("archive_command=")' <<<"$config" >/dev/null; then bad "postgres has no WAL archiving (archive_mode/archive_command)"; fi
jobs=$(jq -r '.services.walship.environment.WALSHIP_JOBS // ""' <<<"$config")
for job in ship base dr-copy; do
  if [[ ,$jobs, != *",$job,"* ]]; then bad "walship job $job missing from WALSHIP_JOBS ('$jobs'); the nightly DR copy needs ship,base,dr-copy"; fi
done

# 5. Alerts cover disk, WAL, backup age, replication lag, bucket usage and DR age.
for name in DuolifyDiskLow DuolifyWALArchiveFailing DuolifyWALSpoolBacklog DuolifyBackupStale DuolifyReplicationLag DuolifyBucketUsageHigh DuolifyDRCopyStale; do
  if ! grep -q "alert: $name\$" "$alerts"; then bad "alert $name missing from $(basename "$alerts")"; fi
done

if ((${#problems[@]} > 0)); then
  printf 'deploy-plan: %s\n' "${problems[@]}" >&2
  exit 1
fi
echo "deploy-plan: ok"
