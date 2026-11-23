#!/usr/bin/env bash
# Unit tests for deploy-plan.sh: the real overlay must pass, and each rule must reject
# exactly one mutated copy of it.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
plan=$here/deploy-plan.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0

# A real environment file has no template placeholders left: the operator pasted the provider keys and
# `selfhost.sh init` generated the secrets and the age recipient (the public key from the age docs).
fill_env() {
  sed -i -e 's#^BACKUP_AGE_RECIPIENT=.*#BACKUP_AGE_RECIPIENT=age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p#' \
    -e 's#^S3_ACCESS_KEY_ID=.*#S3_ACCESS_KEY_ID=app-key-id#' -e 's#^BACKUP_S3_ACCESS_KEY_ID=.*#BACKUP_S3_ACCESS_KEY_ID=backup-key-id#' \
    -e 's#=GENERATED$#=0123456789abcdef#' -e 's#ACCOUNT_ID#abc123#g' "$1"
}

fixture() { # NAME: copy compose.yml, overlay, env, alerts into $work/NAME; prints its dir
  local d=$work/$1
  mkdir -p "$d"
  cp "$here/compose.yml" "$here/compose.prod.yml" "$here/Caddyfile" "$d/"
  cp "$here/.env.prod.example" "$d/env"
  fill_env "$d/env"
  cp "$here/../observability/alerts.yml" "$d/alerts.yml"
  printf '%s' "$d"
}

expect_pass() {
  if bash "$plan" "$(fixture real)" >"$work/out" 2>&1; then printf 'ok   accepts the real overlay\n'
  else printf 'FAIL real overlay rejected:\n'; cat "$work/out"; failures=$((failures + 1)); fi
}

expect_accept() { # NAME MUTATION-COMMAND...: a variation of the real overlay that must still pass
  local name=$1 d
  shift
  d=$(fixture "$name")
  (cd "$d" && "$@")
  if bash "$plan" "$d" >"$work/out" 2>&1; then printf 'ok   accepts %s\n' "$name"
  else printf 'FAIL %s rejected:\n' "$name"; cat "$work/out"; failures=$((failures + 1)); fi
}

expect_reject() { # NAME WORD-IN-MESSAGE MUTATION-COMMAND...
  local name=$1 word=$2 d
  shift 2
  d=$(fixture "$name")
  (cd "$d" && "$@")
  if bash "$plan" "$d" >"$work/out" 2>&1; then printf 'FAIL %s: plan accepted a bad config\n' "$name"; failures=$((failures + 1))
  elif grep -q -- "$word" "$work/out"; then printf 'ok   rejects %s\n' "$name"
  else printf 'FAIL %s: rejected without naming %s:\n' "$name" "$word"; cat "$work/out"; failures=$((failures + 1)); fi
}

expect_pass
# Private PostHog credentials must reach the worker, never the web image.
d=$(fixture posthog-forwarding)
printf 'POSTHOG_PROJECT_ID=project-test\nPOSTHOG_PERSONAL_API_KEY=private-test\n' >> "$d/env"
rendered=$(env -u BACKUP_AGE_RECIPIENT docker compose --project-directory "$d" -f "$d/compose.yml" -f "$d/compose.prod.yml" --env-file "$d/env" --profile '*' config --format json)
if jq -e '.services.worker.environment.POSTHOG_PROJECT_ID == "project-test" and .services.worker.environment.POSTHOG_PERSONAL_API_KEY == "private-test" and ((.services.web.environment // {}) | has("POSTHOG_PERSONAL_API_KEY") | not)' <<<"$rendered" >/dev/null; then printf 'ok   PostHog private credentials reach worker only\n'; else printf 'FAIL PostHog private credentials are not isolated to worker\n'; failures=$((failures+1)); fi
# The worker publishes generated lessons, narration and example decks to the application bucket
# (lessons queue: player.NewS3 in services/api/cmd/api/main.go), so it needs the application key.
for f in "$d/compose.yml" "$d/compose.yml -f $d/compose.prod.yml"; do
  # shellcheck disable=SC2086
  worker=$(env -u BACKUP_AGE_RECIPIENT docker compose --project-directory "$d" -f $f --env-file "$d/env" --profile '*' config --format json | jq -r '.services.worker.environment | "\(.S3_ACCESS_KEY_ID // "")|\(.S3_SECRET_ACCESS_KEY // "")"')
  if [[ $worker != "|"* && $worker != *"|" ]]; then printf 'ok   worker has the application object-store key (%s)\n' "${f##*/}"
  else printf 'FAIL worker has no application object-store key (%s): generated lessons cannot be published\n' "${f##*/}"; failures=$((failures+1)); fi
done
expect_reject missing-app-versioning S3_VERSIONING sed -i "/^S3_VERSIONING=/d" env
expect_reject invalid-app-versioning S3_VERSIONING sed -i "s/^S3_VERSIONING=.*/S3_VERSIONING=unknown/" env
expect_reject missing-app-key S3_ACCESS_KEY_ID sed -i '/^S3_ACCESS_KEY_ID=/d' env
expect_reject missing-app-secret S3_SECRET_ACCESS_KEY sed -i '/^S3_SECRET_ACCESS_KEY=/d' env
expect_reject drain-longer-than-engine-grace ENGINE_DRAIN_TIMEOUT_SECS sh -c "echo ENGINE_DRAIN_TIMEOUT_SECS=300 >> env"
expect_reject drain-longer-than-api-grace API_DRAIN_TIMEOUT sh -c "echo API_DRAIN_TIMEOUT=5m >> env"
expect_reject drain-longer-than-worker-grace WORKER_DRAIN_TIMEOUT sh -c "echo WORKER_DRAIN_TIMEOUT=110s >> env"
expect_reject api-denied-app-key S3_ACCESS_KEY_ID sed -i '0,/<<: \*s3-env/s//<<: *s3-env\n      S3_ACCESS_KEY_ID: ""/' compose.prod.yml
# The checked-in template (no env file next to the compose files) keeps its placeholders and still passes.
mkdir "$work/template" && cp "$here/compose.yml" "$here/compose.prod.yml" "$here/Caddyfile" "$here/.env.prod.example" "$work/template/"
if bash "$plan" "$work/template" >"$work/out" 2>&1; then printf 'ok   accepts the template with its placeholders\n'
else printf 'FAIL template rejected:\n'; cat "$work/out"; failures=$((failures + 1)); fi
expect_reject public-engine-port ENGINE sed -i '/^  engine:/a\    ports: ["50051:50051"]' compose.prod.yml
expect_reject host-network-engine network_mode sed -i '/^  engine:/a\    network_mode: host' compose.prod.yml
expect_reject host-network-api network_mode sed -i '/^  api:/a\    network_mode: host' compose.prod.yml
expect_reject long-syntax-port-api "publish host ports" sed -i '/^  api:/a\    ports: [{target: 8080, published: 18080, protocol: tcp, mode: host}]' compose.prod.yml
expect_reject transaction-pooler pooler sed -i 's#@postgres:5432#@pgbouncer:6432#' compose.yml
expect_reject missing-backup-env BACKUP_S3_ENDPOINT sed -i '/^BACKUP_S3_ENDPOINT=/d' env
expect_reject missing-dr-job dr-copy sed -i 's#dr-copy#noop#g' compose.prod.yml
expect_reject same-location BACKUP_S3_ENDPOINT sed -i 's#^BACKUP_S3_ENDPOINT=.*#BACKUP_S3_ENDPOINT=https://hel1.example-storage.com#' env
expect_reject same-bucket BACKUP_S3_BUCKET sed -i 's#^BACKUP_S3_BUCKET=.*#BACKUP_S3_BUCKET=duolify#' env
expect_reject missing-region S3_REGION sed -i '/^S3_REGION=/d' env
expect_reject missing-alert alert sed -i 's#DuolifyDRCopyStale#Renamed#' alerts.yml
expect_reject no-absent-rule absent sed -i 's#absent(duolify_base_last_success_timestamp_seconds)#vector(0)#' alerts.yml
expect_reject replication-lag-alert DuolifyReplicationLag sh -c "printf '      - alert: DuolifyReplicationLag\n        expr: duolify_pg_replication_lag_bytes > 1\n' >>alerts.yml"

# The API refuses to start when the backup writer shares the application key; fail before pulling images.
expect_reject same-backup-key "separate credentials" sed -i 's#^BACKUP_S3_ACCESS_KEY_ID=.*#BACKUP_S3_ACCESS_KEY_ID=app-key-id#' env
expect_reject same-backup-key-by-default "separate credentials" sh -c "sed -i -e '/^S3_ACCESS_KEY_ID=/d' -e 's#^BACKUP_S3_ACCESS_KEY_ID=.*#BACKUP_S3_ACCESS_KEY_ID=duolify-app#' env"
# deploy.sh never generates a private key: the recipient must already be in the environment file.
expect_reject missing-age-recipient BACKUP_AGE_RECIPIENT sed -i '/^BACKUP_AGE_RECIPIENT=/d' env
expect_reject malformed-age-recipient "age recipient" sed -i 's#^BACKUP_AGE_RECIPIENT=.*#BACKUP_AGE_RECIPIENT=not-an-age-key#' env
expect_reject age-recipient-placeholder "age recipient" sed -i 's#^BACKUP_AGE_RECIPIENT=.*#BACKUP_AGE_RECIPIENT=GENERATED#' env
# The lesson iframe CSP only allows the asset origin; a localhost origin cannot work on a real environment.
expect_reject localhost-asset-origin LESSON_ASSET_ORIGIN sed -i 's#^LESSON_ASSET_ORIGIN=.*#LESSON_ASSET_ORIGIN=http://localhost:9000#' env
expect_reject loopback-asset-origin LESSON_ASSET_ORIGIN sed -i 's#^LESSON_ASSET_ORIGIN=.*#LESSON_ASSET_ORIGIN=https://127.0.0.1:9000#' env
expect_reject plain-http-asset-origin LESSON_ASSET_ORIGIN sed -i 's#^LESSON_ASSET_ORIGIN=.*#LESSON_ASSET_ORIGIN=http://assets.duolify.online#' env
expect_reject unset-asset-origin LESSON_ASSET_ORIGIN sed -i '/^LESSON_ASSET_ORIGIN=/d' env
expect_accept localhost-asset-origin-in-development sh -c "sed -i -e 's#^DUOLIFY_ENV=.*#DUOLIFY_ENV=development#' -e 's#^LESSON_ASSET_ORIGIN=.*#LESSON_ASSET_ORIGIN=http://localhost:9000#' env"
expect_accept staging-env-with-r2-asset-origin sh -c "sed -i -e 's#^DUOLIFY_ENV=.*#DUOLIFY_ENV=staging#' -e 's#^LESSON_ASSET_ORIGIN=.*#LESSON_ASSET_ORIGIN=https://assets.staging.duolify.online#' env"
# Edge settings: the API's CORS allowlist and cookie mode need the exact https origin.
expect_reject missing-public-origin PUBLIC_ORIGIN sed -i '/^PUBLIC_ORIGIN=/d' env
expect_reject http-public-origin PUBLIC_ORIGIN sed -i 's#^PUBLIC_ORIGIN=.*#PUBLIC_ORIGIN=http://duolify.online#' env
expect_reject public-origin-with-path PUBLIC_ORIGIN sed -i 's#^PUBLIC_ORIGIN=.*#PUBLIC_ORIGIN=https://duolify.online/app#' env
expect_reject empty-site-address SITE_ADDRESS sed -i 's#^SITE_ADDRESS=.*#SITE_ADDRESS=#' env
expect_reject plain-http-site-address SITE_ADDRESS sed -i 's#^SITE_ADDRESS=.*#SITE_ADDRESS=:80#' env
expect_accept http-edge-in-development sh -c "sed -i -e 's#^DUOLIFY_ENV=.*#DUOLIFY_ENV=development#' -e 's#^SITE_ADDRESS=.*#SITE_ADDRESS=:80#' -e 's#^PUBLIC_ORIGIN=.*#PUBLIC_ORIGIN=http://localhost#' env"
# A filled environment has no template placeholder left.
expect_reject placeholder-key CHANGE_ME sed -i 's#^S3_ACCESS_KEY_ID=.*#S3_ACCESS_KEY_ID=CHANGE_ME_APP#' env
expect_reject placeholder-secret GENERATED sed -i 's#^JWT_SECRET=.*#JWT_SECRET=GENERATED#' env
expect_reject placeholder-endpoint ACCOUNT_ID sed -i 's#^S3_ENDPOINT=.*#S3_ENDPOINT=https://ACCOUNT_ID.r2.cloudflarestorage.com#' env
# The 4 GB VM: the memory limits of the services that run there must leave room for the OS and page cache.
expect_reject oversized-postgres "memory limits" sed -i 's#^POSTGRES_MEMORY=.*#POSTGRES_MEMORY=3G#' env
# Pool sizes against max_connections (3 API instances during a roll + worker + 8 spare).
expect_reject oversized-api-pool max_connections sh -c "printf 'API_DB_POOL_MAX_CONNS=40\n' >>env"
expect_reject no-max-connections max_connections sed -i '/max_connections=60/d' compose.prod.yml
expect_accept smaller-pools sh -c "printf 'API_DB_POOL_MAX_CONNS=6\nWORKER_DB_POOL_MAX_CONNS=8\n' >>env"

((failures == 0)) || { printf '%s failure(s)\n' "$failures" >&2; exit 1; }
