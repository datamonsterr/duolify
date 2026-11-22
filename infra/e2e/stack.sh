#!/usr/bin/env bash
# Disposable full-stack e2e environment: infra/deploy/compose.yml + infra/e2e/compose.yml
# as Compose project `duolify-e2e` (never touches a `duolify-selfhost` stack).
#
#   stack.sh up            build images (tag e2e), start, wait for health
#   stack.sh down          stop and delete containers AND volumes (all e2e data)
#   stack.sh logs [svc..]  print recent logs
#   stack.sh code <email>  print every sign-in code mailed to <email>, oldest first (dev log mailer)
#   stack.sh grant-credits <learner-id> <n>
#                          record a purchase of <n> credits for a learner. Card payments are
#                          off in every non-test build (STRIPE_MODE), so this stands in for a
#                          completed purchase; flows that need paid credits say so.
#   stack.sh url           print the browser base URL
#   stack.sh compose ...   any docker compose command against this stack
#
# Provider mode: mock (deterministic, free) by default. E2E_LIVE=1 runs the engine with
# ENGINE_PROVIDER_MODE=live and needs OPENROUTER_API_KEY in the caller's environment; the
# key is passed to Compose through the process environment only, never written to disk.
set -euo pipefail

e2e_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$e2e_dir/../.." && pwd)
deploy_dir=$repo_root/infra/deploy
state_dir=${E2E_STACK_STATE_DIR:-$e2e_dir/.state}
env_file=$state_dir/.env
port=${E2E_STACK_PORT:-18471}
project=${E2E_STACK_PROJECT:-duolify-e2e}

die() { printf 'e2e-stack: %s\n' "$*" >&2; exit 1; }
say() { printf '==> %s\n' "$*" >&2; }

live() { [[ ${E2E_LIVE:-} == 1 || ${E2E_LIVE:-} == true ]]; }

# Like selfhost.sh: Compose sees only the env file plus an explicit allowlist, never the
# development .env that mise exports into every task.
compose() {
  [[ -f $env_file ]] || die "missing $env_file; run: $0 up"
  local keep=(PATH HOME USER DOCKER_HOST DOCKER_CONTEXT DOCKER_CONFIG DOCKER_CERT_PATH DOCKER_TLS_VERIFY XDG_RUNTIME_DIR BUILDKIT_PROGRESS E2E_LESSON_TEST_TRACER_ENABLED)
  if live; then keep+=(OPENROUTER_API_KEY); fi
  local pass=() name
  for name in "${keep[@]}"; do
    if [[ -n ${!name:-} ]]; then pass+=("$name=${!name}"); fi
  done
  env -i "${pass[@]}" docker compose --project-name "$project" --project-directory "$deploy_dir" \
    --file "$deploy_dir/compose.yml" --file "$e2e_dir/compose.yml" --env-file "$env_file" "$@"
}

setting() { sed -n "s/^$1=//p" "$env_file" | tail -n 1; }

set_setting() { # KEY VALUE (value must not contain '|')
  if grep -q "^$1=" "$env_file"; then
    sed -i.bak "s|^$1=.*|$1=$2|" "$env_file" && rm -f "$env_file.bak"
  else
    printf '%s=%s\n' "$1" "$2" >>"$env_file"
  fi
}

cmd_init() {
  mkdir -p "$state_dir" && chmod 700 "$state_dir"
  # Reuse the self-host generator for fresh random secrets in our own env file.
  SELFHOST_ENV_FILE=$env_file bash "$deploy_dir/selfhost.sh" init >&2
  set_setting IMAGE_REGISTRY duolify
  set_setting IMAGE_TAG "${E2E_STACK_IMAGE_TAG:-e2e}"
  set_setting BIND_ADDRESS 127.0.0.1
  set_setting HTTP_PORT "$port"
  set_setting HTTPS_PORT "${E2E_STACK_HTTPS_PORT:-18472}"
  set_setting SITE_ADDRESS :80
  set_setting PUBLIC_ORIGIN "http://localhost:$port"
  set_setting DUOLIFY_ENV development
  set_setting ANALYTICS_OPT_OUT true
  if live; then
    [[ -n ${OPENROUTER_API_KEY:-} ]] || die "E2E_LIVE=1 needs OPENROUTER_API_KEY in the environment"
    set_setting ENGINE_PROVIDER_MODE live
  else
    set_setting ENGINE_PROVIDER_MODE mock
  fi
}

ensure_age_recipient() { # the worker refuses to start without one; the identity is throwaway
  if [[ $(setting BACKUP_AGE_RECIPIENT) != GENERATED ]]; then return; fi
  local identity=$state_dir/backup-age-identity.txt image
  image="$(setting IMAGE_REGISTRY)/worker:$(setting IMAGE_TAG)"
  (umask 077 && docker run --rm --network none --entrypoint age-keygen "$image" >"$identity" 2>/dev/null)
  set_setting BACKUP_AGE_RECIPIENT "$(sed -n 's/^# public key: //p' "$identity")"
}

cmd_up() {
  cmd_init
  if [[ ${E2E_STACK_NO_BUILD:-} != 1 ]]; then
    say "building images (tag $(setting IMAGE_TAG))"
    compose build
  fi
  ensure_age_recipient
  say "starting $project (engine provider mode: $(setting ENGINE_PROVIDER_MODE))"
  compose up --detach --wait --wait-timeout "${E2E_STACK_WAIT_TIMEOUT:-300}" --remove-orphans
  say "stack is up: $(setting PUBLIC_ORIGIN)"
}

cmd_code() {
  local email=${1:?usage: stack.sh code <email>} lines
  # LogMailer (services/api/internal/accounts/mailer.go) logs one JSON line per code.
  lines=$(compose logs --no-color --no-log-prefix api 2>/dev/null | grep -F 'DEV ONLY sign-in mail' | grep -F "\"email\":\"$email\"" || true)
  [[ -n $lines ]] || die "no sign-in code for $email in the api log"
  sed -n 's/.*"code":"\([0-9A-Za-z]*\)".*/\1/p' <<<"$lines"
}

cmd_grant_credits() {
  local learner=${1:?usage: stack.sh grant-credits <learner-id> <amount>} amount=${2:?amount}
  [[ $learner =~ ^[0-9a-f-]{36}$ && $amount =~ ^[0-9]+$ ]] || die "grant-credits: bad learner id or amount"
  compose exec -T postgres psql -v ON_ERROR_STOP=1 -q -U duolify -d duolify -c \
    "INSERT INTO credit_entries(learner_id, amount, kind, reference, provenance) VALUES ('$learner', $amount, 'purchase', 'e2e-grant-' || gen_random_uuid(), '{\"source\":\"e2e-stack\"}')" >/dev/null
  # Drop the cached wallet (credits.Service.InvalidateCache) so the next read sees the grant.
  compose exec -T redis redis-cli DEL "credits:wallet:$learner" >/dev/null
}

case ${1:-} in
  up) cmd_up ;;
  down) if [[ -f $env_file ]]; then compose --profile tools down --volumes --remove-orphans; fi ;;
  logs) shift; compose logs --no-color --tail "${E2E_STACK_LOG_TAIL:-300}" "$@" ;;
  code) shift; cmd_code "$@" ;;
  grant-credits) shift; cmd_grant_credits "$@" ;;
  url) printf 'http://localhost:%s\n' "$port" ;;
  compose) shift; compose "$@" ;;
  *) sed -n '2,18p' "$0" >&2; exit 2 ;;
esac
