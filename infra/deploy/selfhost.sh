#!/usr/bin/env bash
# Operate the self-hosted Duolify stack (infra/deploy/compose.yml). See docs/infra/selfhost.md.
#
#   selfhost.sh init           create infra/deploy/.env with fresh random secrets (idempotent)
#   selfhost.sh up             build or pull images, then start everything and wait for health
#   selfhost.sh down           stop the stack; named volumes (all data) are kept
#   selfhost.sh logs [svc...]  follow logs
#   selfhost.sh smoke          health, routing, CORS, SSE streaming and web checks via the proxy
#   selfhost.sh backup         make sure today's encrypted database backup exists
#   selfhost.sh restore-drill  restore the newest backup into a throwaway Postgres and verify it
#   selfhost.sh compose ...    any other docker compose command against this stack
set -euo pipefail

deploy_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$deploy_dir/../.." && pwd)
env_file=${SELFHOST_ENV_FILE:-$deploy_dir/.env}
identity_file=${SELFHOST_AGE_IDENTITY_FILE:-$deploy_dir/secrets/backup-age-identity.txt}

die() { printf 'selfhost: %s\n' "$*" >&2; exit 1; }
say() { printf '==> %s\n' "$*"; }

# Compose sees only the env file, never variables exported by the caller (mise loads
# the development .env into every task; its values must not leak into this stack).
compose() {
  [[ -f $env_file ]] || die "missing $env_file; run: $0 init"
  local keep=(PATH HOME USER DOCKER_HOST DOCKER_CONTEXT DOCKER_CONFIG DOCKER_CERT_PATH DOCKER_TLS_VERIFY XDG_RUNTIME_DIR BUILDKIT_PROGRESS)
  local pass=() name
  for name in "${keep[@]}"; do
    if [[ -n ${!name:-} ]]; then pass+=("$name=${!name}"); fi
  done
  env -i "${pass[@]}" docker compose --project-directory "$deploy_dir" \
    --file "$deploy_dir/compose.yml" --env-file "$env_file" "$@"
}

setting() { # value of KEY in the env file
  sed -n "s/^$1=//p" "$env_file" | tail -n 1
}

set_setting() { # KEY VALUE (value must not contain '|')
  if grep -q "^$1=" "$env_file"; then
    sed -i.bak "s|^$1=.*|$1=$2|" "$env_file" && rm -f "$env_file.bak"
  else
    printf '%s=%s\n' "$1" "$2" >>"$env_file"
  fi
}

random_hex() { head -c 32 /dev/urandom | od -An -vtx1 | tr -d ' \n'; }

image() { printf '%s/%s:%s' "$(setting IMAGE_REGISTRY)" "$1" "$(setting IMAGE_TAG)"; }

cmd_init() {
  if [[ ! -f $env_file ]]; then
    (umask 077 && cp "$deploy_dir/.env.example" "$env_file")
    say "created $env_file"
  fi
  local key
  sed -n 's/^\([A-Z0-9_]*\)=GENERATED$/\1/p' "$env_file" | while read -r key; do
    if [[ $key != BACKUP_AGE_RECIPIENT ]]; then set_setting "$key" "$(random_hex)"; fi
  done
}

# Needs the worker image (it ships age-keygen), so it runs after images exist.
ensure_age_identity() {
  if [[ $(setting BACKUP_AGE_RECIPIENT) != GENERATED ]]; then return; fi
  [[ ! -e $identity_file ]] || die "$identity_file exists but BACKUP_AGE_RECIPIENT is unset; restore the matching recipient"
  mkdir -p "$(dirname "$identity_file")" && chmod 700 "$(dirname "$identity_file")"
  (umask 077 && docker run --rm --network none --entrypoint age-keygen "$(image worker)" >"$identity_file" 2>/dev/null)
  set_setting BACKUP_AGE_RECIPIENT "$(sed -n 's/^# public key: //p' "$identity_file")"
  say "backup age identity written to $identity_file (store a copy off this machine)"
}

cmd_up() {
  cmd_init
  if [[ $(setting IMAGE_TAG) == local ]]; then
    say "building images from $repo_root"
    compose build
  else
    say "pulling $(setting IMAGE_REGISTRY) images tagged $(setting IMAGE_TAG)"
    compose pull --ignore-buildable --policy always 2>/dev/null || compose pull
  fi
  ensure_age_identity
  compose up --detach --wait --remove-orphans
  say "stack is up: $(setting PUBLIC_ORIGIN)"
}

proxy_url() { # base URL plus curl options reaching the proxy on this host
  local site port
  site=$(setting SITE_ADDRESS)
  site=${site#http://}
  if [[ $site == :* || $site == "" ]] || [[ $(setting SITE_ADDRESS) == http://* ]]; then
    port=$(setting HTTP_PORT)
    base="http://127.0.0.1:${port:-80}"
    curl_opts=()
  else
    port=$(setting HTTPS_PORT)
    base="https://$site:${port:-443}"
    curl_opts=(--resolve "$site:${port:-443}:127.0.0.1")
  fi
}

check() { printf '  %-52s' "$1"; }
ok() { printf 'ok\n'; }
fail() { printf 'FAIL\n'; printf '    %s\n' "$@" >&2; failures=$((failures + 1)); }

cmd_smoke() {
  local base curl_opts failures=0 service health body first published origin script
  proxy_url
  origin=$(setting PUBLIC_ORIGIN)
  say "smoke testing $base"

  for service in proxy postgres redis objectstore engine api worker; do
    check "$service healthy"
    health=$(compose ps --format '{{.Health}}' "$service" 2>/dev/null || true)
    if [[ $health == healthy ]]; then ok; else fail "health: ${health:-not running}"; fi
  done

  check "only the proxy publishes host ports"
  published=$(compose ps --format '{{.Service}} {{.Ports}}' | grep -- '->' | awk '$1 != "proxy" {print $1}' || true)
  if [[ -z $published ]]; then ok; else fail "published: $published"; fi

  check "API readiness (postgres, redis, object storage, engine)"
  if compose exec -T api /api healthcheck >/dev/null 2>&1; then ok; else fail "/api healthcheck failed"; fi

  check "GET /v1/features through the proxy"
  body=$(curl -fsS "${curl_opts[@]}" "$base/v1/features" 2>&1 || true)
  if [[ $body == *marketplace_enabled* ]]; then ok; else fail "$body"; fi

  check "browser origin $origin allowed by the API"
  if curl -fsS -o /dev/null "${curl_opts[@]}" -H "Origin: $origin" "$base/v1/features"; then ok; else fail "API rejected $origin; check PUBLIC_ORIGIN"; fi

  check "SSE first event arrives unbuffered (<1.5s)"
  first=$(timeout 1.5 curl -sSN "${curl_opts[@]}" "$base/v1/stream-check" 2>/dev/null | head -n 1 || true)
  if [[ $first == "event: ping" ]]; then ok; else fail "first line: ${first:-<none within 1.5s>}"; fi

  check "SSE stream completes through the proxy"
  body=$(curl -sSN --max-time 10 "${curl_opts[@]}" "$base/v1/stream-check" 2>&1 || true)
  if [[ $body == *'"seq":2'* ]]; then ok; else fail "$body"; fi

  check "web client served at /"
  body=$(curl -fsS "${curl_opts[@]}" "$base/" 2>&1 || true)
  if [[ $body == *'id="root"'* ]]; then ok; else fail "index.html missing"; fi

  check "SPA fallback for deep links"
  if curl -fsS "${curl_opts[@]}" "$base/courses/smoke-check" | grep -q 'id="root"'; then ok; else fail "deep link did not return index.html"; fi

  check "web bundle calls the API on its own origin"
  script=$(printf '%s' "$body" | grep -o '/assets/[^"]*\.js' | head -n 1 || true)
  if [[ -n $script ]] && curl -fsS "${curl_opts[@]}" "$base$script" | grep -q 'localhost:8080'; then
    fail "bundle $script still targets localhost:8080 (VITE_API_URL)"
  elif [[ -n $script ]]; then ok; else fail "no /assets/*.js in index.html"; fi

  if ((failures > 0)); then die "$failures smoke check(s) failed"; fi
  say "smoke passed"
}

newest_dump() { # newest general (non-financial) encrypted dump key, or empty
  compose run --rm -T --no-deps backup-reader s3api list-objects-v2 --bucket duolify-backups \
    --query 'Contents[].Key' --output text 2>/dev/null | tr '\t' '\n' \
    | grep -E "^${1:-[0-9]{4}-[0-9]{2}-[0-9]{2}}/schema-[0-9]+\.dump\.age$" | sort | tail -n 1 || true
}

cmd_backup() {
  local today key attempt
  today=$(date -u +%F)
  key=$(newest_dump "$today")
  if [[ -z $key ]]; then
    say "no backup for $today yet; restarting the worker (it backs up on start, then nightly at 02:00 UTC)"
    compose restart worker >/dev/null
    for attempt in $(seq 1 60); do
      key=$(newest_dump "$today")
      [[ -z $key ]] || break
      [[ $attempt != 60 ]] || die "no backup after 5 minutes; check: $0 logs worker"
      sleep 5
    done
  fi
  say "encrypted backup present: s3://duolify-backups/$key"
}

cmd_restore_drill() {
  local key
  [[ -r $identity_file ]] || die "age identity $identity_file not readable; the drill needs the backup private key"
  key=$(newest_dump)
  [[ -n $key ]] || die "no backup found; run: $0 backup"
  say "restoring s3://duolify-backups/$key into a throwaway, network-less Postgres"
  # Plaintext exists only in this pipe and the disposable container; never on disk.
  compose run --rm -T --no-deps backup-reader s3 cp "s3://duolify-backups/$key" - \
    | docker run --rm -i --network none --user "$(id -u):$(id -g)" \
        --volume "$identity_file:/identity:ro" --entrypoint age "$(image worker)" --decrypt --identity /identity \
    | bash "$repo_root/infra/backup/restore-drill.sh" -
  say "restore drill passed for $key"
}

case ${1:-} in
  init) cmd_init ;;
  up) cmd_up ;;
  down) compose --profile tools down --remove-orphans ;;
  logs) shift; compose logs --follow --tail 100 "$@" ;;
  smoke) cmd_smoke ;;
  backup) cmd_backup ;;
  restore-drill) cmd_restore_drill ;;
  compose) shift; compose "$@" ;;
  *) sed -n '2,12p' "$0" >&2; exit 2 ;;
esac
