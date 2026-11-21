#!/usr/bin/env bash
# Unit tests for deploy-plan.sh: the real overlay must pass, and each rule must reject
# exactly one mutated copy of it.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
plan=$here/deploy-plan.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0

fixture() { # NAME: copy compose.yml, overlay, env, alerts into $work/NAME; prints its dir
  local d=$work/$1
  mkdir -p "$d"
  cp "$here/compose.yml" "$here/compose.prod.yml" "$here/Caddyfile" "$d/"
  cp "$here/.env.prod.example" "$d/env"
  cp "$here/../observability/alerts.yml" "$d/alerts.yml"
  printf '%s' "$d"
}

expect_pass() {
  if bash "$plan" "$(fixture real)" >"$work/out" 2>&1; then printf 'ok   accepts the real overlay\n'
  else printf 'FAIL real overlay rejected:\n'; cat "$work/out"; failures=$((failures + 1)); fi
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
expect_reject public-engine-port ENGINE sed -i '/^  engine:/a\    ports: ["50051:50051"]' compose.prod.yml
expect_reject host-network-engine host-network sed -i '/^  engine:/a\    network_mode: host' compose.prod.yml
expect_reject host-network-api host-network sed -i '/^  api:/a\    network_mode: host' compose.prod.yml
expect_reject long-syntax-port-api "publish host ports" sed -i '/^  api:/a\    ports: [{target: 8080, published: 18080, protocol: tcp, mode: host}]' compose.prod.yml
expect_reject transaction-pooler pooler sed -i 's#@postgres:5432#@pgbouncer:6432#' compose.yml
expect_reject missing-backup-env BACKUP_S3_ENDPOINT sed -i '/^BACKUP_S3_ENDPOINT=/d' env
expect_reject missing-dr-job dr-copy sed -i 's#dr-copy#noop#g' compose.prod.yml
expect_reject same-location BACKUP_S3_ENDPOINT sed -i 's#^BACKUP_S3_ENDPOINT=.*#BACKUP_S3_ENDPOINT=https://hel1.your-objectstorage.com#' env
expect_reject missing-alert alert sed -i 's#DuolifyDRCopyStale#Renamed#' alerts.yml
expect_reject no-absent-rule absent sed -i 's#absent(duolify_base_last_success_timestamp_seconds)#vector(0)#' alerts.yml
expect_reject replication-lag-alert DuolifyReplicationLag sh -c "printf '      - alert: DuolifyReplicationLag\n        expr: duolify_pg_replication_lag_bytes > 1\n' >>alerts.yml"

((failures == 0)) || { printf '%s failure(s)\n' "$failures" >&2; exit 1; }
