#!/usr/bin/env bash
# Unit tests for deploy.sh's argument validation (digest pinning). Nothing is started: every case
# either fails validation or stops at DEPLOY_VALIDATE_ONLY=1 before touching Docker or the env file.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
failures=0
d1=$(printf 'a%.0s' {1..64})
good=(
  "api=ghcr.io/o/r/api@sha256:$d1"
  "worker=ghcr.io/o/r/worker@sha256:$d1"
  "engine=ghcr.io/o/r/engine@sha256:$d1"
  "web=ghcr.io/o/r/web@sha256:$d1"
  "backup-init=ghcr.io/o/r/backup-init@sha256:$d1"
)

run() { DEPLOY_VALIDATE_ONLY=1 SELFHOST_ENV_FILE=/nonexistent bash "$here/deploy.sh" "$@" 2>&1 <"${stdin:-/dev/null}"; }
expect() { # NAME rc|ok WORD args...
  local name=$1 want=$2 word=$3 out rc=0
  shift 3
  out=$(run "$@") || rc=$?
  if [[ $want == ok ]]; then
    if ((rc == 0)); then printf 'ok   %s\n' "$name"; else printf 'FAIL %s: rc=%s\n%s\n' "$name" "$rc" "$out"; failures=$((failures + 1)); fi
  elif ((rc == 2)) && grep -q -- "$word" <<<"$out"; then printf 'ok   %s\n' "$name"
  else printf 'FAIL %s: rc=%s (wanted 2 naming "%s")\n%s\n' "$name" "$rc" "$word" "$out"; failures=$((failures + 1)); fi
}

expect "accepts a sha tag with five digest-pinned images" ok - sha-abcdef1 "${good[@]}"
expect "rejects a deploy with no images" rej digest sha-abcdef1
expect "rejects a bad commit tag" rej tag latest "${good[@]}"
expect "rejects a tag-only image reference" rej digest sha-abcdef1 api=ghcr.io/o/r/api:sha-abcdef1 "${good[@]:1}"
expect "rejects a tag plus digest reference" rej digest sha-abcdef1 api=ghcr.io/o/r/api:sha-abcdef1@sha256:"$d1" "${good[@]:1}"
expect "rejects a truncated digest" rej digest sha-abcdef1 api=ghcr.io/o/r/api@sha256:abc "${good[@]:1}"
expect "rejects a missing image" rej backup-init sha-abcdef1 "${good[@]:0:4}"
expect "rejects an unknown image name" rej unknown sha-abcdef1 "${good[@]}" evil=ghcr.io/o/r/evil@sha256:"$d1"
expect "rejects shell metacharacters in a reference" rej digest sha-abcdef1 'api=ghcr.io/o/r/api@sha256:'"$d1"';rm -rf /' "${good[@]:1}"

# Registry login: a GitHub login plus a token on stdin (never an argument).
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
printf 'ghs_faketoken\n' >"$tmp/token"
DEPLOY_REGISTRY_USER='github-actions[bot]' stdin=$tmp/token expect "accepts a registry user with a token on stdin" ok - sha-abcdef1 "${good[@]}"
DEPLOY_REGISTRY_USER=octocat expect "rejects a registry user without a token on stdin" rej token sha-abcdef1 "${good[@]}"
DEPLOY_REGISTRY_USER='bad;user' stdin=$tmp/token expect "rejects a registry user that is not a GitHub login" rej login sha-abcdef1 "${good[@]}"

# The login uses a throwaway DOCKER_CONFIG, reads the token from stdin only, and logs out even when
# the pull fails. A stub `docker` records calls; `compose pull` fails so nothing else runs.
mkdir -p "$tmp/bin" "$tmp/deploy"
cp "$here/deploy.sh" "$here/selfhost.sh" "$tmp/deploy/"
printf 'IMAGE_TAG=local\n' >"$tmp/deploy/.env"
cat >"$tmp/bin/docker" <<STUB
#!/usr/bin/env bash
echo "argv \$* config=\${DOCKER_CONFIG:-none}" >>"$tmp/calls"
case \$1 in
  login) [[ -d \${DOCKER_CONFIG:-/nonexistent} ]] && echo "stdin \$(cat)" >>"$tmp/calls" ;;
  logout) echo logout >>"$tmp/calls" ;;
  compose) exit 1 ;;
esac
STUB
chmod +x "$tmp/bin/docker"
rc=0
printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp/plan-ok.sh"
PATH="$tmp/bin:$PATH" DEPLOY_PLAN="$tmp/plan-ok.sh" DEPLOY_REGISTRY_USER=octocat bash "$tmp/deploy/deploy.sh" sha-abcdef1 "${good[@]}" <"$tmp/token" >/dev/null 2>&1 || rc=$?
calls=$(cat "$tmp/calls" 2>/dev/null || true)
config_dir=$(sed -n 's/^argv login .* config=//p' <<<"$calls")
if ((rc != 0)) && grep -q '^stdin ghs_faketoken$' <<<"$calls" && grep -q '^logout$' <<<"$calls" \
  && ! grep '^argv' <<<"$calls" | grep -q ghs_faketoken && [[ -n $config_dir && $config_dir != none && ! -e $config_dir ]]; then
  printf 'ok   registry login via stdin in a temporary config, logged out after a failed pull\n'
else printf 'FAIL registry login/logout (rc=%s):\n%s\n' "$rc" "$calls"; failures=$((failures + 1)); fi

# The plan runs against the real env file before anything is pulled or migrated: a bad .env stops the
# deploy with the plan's message, with no docker login, pull, build or migrate.
mkdir -p "$tmp/vm/deploy" "$tmp/vm/observability" "$tmp/vm/bin"
cp "$here/deploy.sh" "$here/selfhost.sh" "$here/deploy-plan.sh" "$here/compose.yml" "$here/compose.prod.yml" "$here/Caddyfile" "$tmp/vm/deploy/"
cp "$here/../observability/alerts.yml" "$tmp/vm/observability/"
cp "$here/.env.prod.example" "$tmp/vm/deploy/.env"
sed -i -e 's#^BACKUP_AGE_RECIPIENT=.*#BACKUP_AGE_RECIPIENT=age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p#' \
  -e 's#^S3_ACCESS_KEY_ID=.*#S3_ACCESS_KEY_ID=app-key-id#' -e 's#^BACKUP_S3_ACCESS_KEY_ID=.*#BACKUP_S3_ACCESS_KEY_ID=app-key-id#' \
  -e 's#=GENERATED$#=0123456789abcdef#' -e 's#ACCOUNT_ID#abc123#g' "$tmp/vm/deploy/.env"
real_docker=$(command -v docker)
cat >"$tmp/vm/bin/docker" <<STUB
#!/usr/bin/env bash
case "\$*" in
  *" config "*|*" config") exec "$real_docker" "\$@" ;;
  *) echo "argv \$*" >>"$tmp/vm/calls" ;;
esac
exit 1
STUB
chmod +x "$tmp/vm/bin/docker"
rc=0
out=$(PATH="$tmp/vm/bin:$PATH" bash "$tmp/vm/deploy/deploy.sh" sha-abcdef1 "${good[@]}" 2>&1 </dev/null) || rc=$?
if ((rc == 1)) && grep -q 'separate credentials' <<<"$out" && grep -q 'nothing was pulled' <<<"$out" && [[ ! -s $tmp/vm/calls ]]; then
  printf 'ok   a bad .env stops the deploy before any pull or migrate\n'
else printf 'FAIL bad .env did not stop the deploy early (rc=%s):\n%s\n%s\n' "$rc" "$out" "$(cat "$tmp/vm/calls" 2>/dev/null)"; failures=$((failures + 1)); fi
# With a valid .env the plan passes and the deploy goes on to the pull (which this stub fails).
sed -i 's#^BACKUP_S3_ACCESS_KEY_ID=.*#BACKUP_S3_ACCESS_KEY_ID=backup-key-id#' "$tmp/vm/deploy/.env"
rc=0
out=$(PATH="$tmp/vm/bin:$PATH" bash "$tmp/vm/deploy/deploy.sh" sha-abcdef1 "${good[@]}" 2>&1 </dev/null) || rc=$?
if ((rc != 0)) && grep -q 'deploy-plan: ok' <<<"$out" && grep -q ' pull ' "$tmp/vm/calls" 2>/dev/null; then
  printf 'ok   a valid .env passes the plan and the deploy proceeds to the pull\n'
else printf 'FAIL valid .env (rc=%s):\n%s\n%s\n' "$rc" "$out" "$(cat "$tmp/vm/calls" 2>/dev/null)"; failures=$((failures + 1)); fi

# The Compose files must honour the pinned references.
out=$(API_IMAGE=ghcr.io/o/r/api@sha256:$d1 WORKER_IMAGE=ghcr.io/o/r/worker@sha256:$d1 ENGINE_IMAGE=ghcr.io/o/r/engine@sha256:$d1 \
  WEB_IMAGE=ghcr.io/o/r/web@sha256:$d1 BACKUP_INIT_IMAGE=ghcr.io/o/r/backup-init@sha256:$d1 \
  docker compose --project-directory "$here" --file "$here/compose.yml" --file "$here/compose.prod.yml" --env-file "$here/.env.prod.example" --profile '*' config --format json \
  | jq -r '[.services.api, .services.migrate, .services.worker, .services.engine, .services.proxy, .services["backup-init"]] | map(.image) | join(" ")')
if [[ $(tr ' ' '\n' <<<"$out" | grep -c "@sha256:$d1\$") == 6 ]]; then printf 'ok   compose uses the pinned references\n'
else printf 'FAIL compose did not use the pinned references: %s\n' "$out"; failures=$((failures + 1)); fi

# Convergence (a failed deploy must not poison the next one). A stateful fake `docker` keeps one file per
# container (service, state, exit code, health, generation) and models what Compose does: `up --wait`
# fails while ANY container of the service is unhealthy or restarting (the staging failure "container
# duolify-selfhost-api-3 is unhealthy"), and a bad new image produces a restarting container.
fake=$tmp/fake
mkdir -p "$fake/bin" "$fake/deploy"
cp "$here/deploy.sh" "$fake/deploy/"
# shellcheck disable=SC2016 # the stub's own $1 must stay literal
printf '#!/usr/bin/env bash\n[[ $1 == compose ]] && { shift; exec docker compose "$@"; }\n[[ $1 == smoke ]] && exit ${FAKE_SMOKE_RC:-0}\nexit 1\n' >"$fake/deploy/selfhost.sh"
printf '#!/usr/bin/env bash\nexit 0\n' >"$fake/plan-ok.sh"
cat >"$fake/lib.sh" <<'LIB'
f() { sed -n "s/^$2=//p" "$st/c/$1"; }
mk() { # service state exit health gen -> container name <service>-<n>
  local n; n=$(($(cat "$st/seq-$1" 2>/dev/null || echo 0) + 1)); echo "$n" >"$st/seq-$1"
  printf 'service=%s\nstate=%s\nexit=%s\nhealth=%s\ngen=%s\n' "$1" "$2" "$3" "$4" "$5" >"$st/c/$1-$n"
}
list() { ls "$st/c" | sort -t- -k1,1 -k2,2n; }
LIB
cat >"$fake/bin/docker" <<'STUB'
#!/usr/bin/env bash
st=$FAKE_STATE
echo "$*" >>"$st/calls"
. "$(dirname "$0")/../lib.sh"
case $1 in
  stop) echo "stop ${*: -1}" >>"$st/events" ;;
  rm) for a in "${@:2}"; do [[ $a == -* ]] || { echo "rm $a" >>"$st/events"; rm -f "$st/c/$a"; }; done ;;
  compose)
    shift; sub=$1; shift
    case $sub in
      pull | build | run) ;;
      ps)
        all=0 quiet=0 fmt='' svc=''
        while (($#)); do case $1 in -a | --all) all=1 ;; -q | --quiet) quiet=1 ;; --format) fmt=$2; shift ;; *) svc=$1 ;; esac; shift; done
        for n in $(list); do
          [[ -z $svc || $(f "$n" service) == "$svc" ]] || continue
          state=$(f "$n" state)
          ((all)) || [[ $state == running || $state == restarting ]] || continue
          if ((quiet)); then echo "$n"; else echo "$n|$(f "$n" service)|$state|$(f "$n" exit)|$(f "$n" health)"; fi
        done ;;
      up)
        scale='' svcs=()
        while (($#)); do case $1 in --scale) scale=${2#*=}; shift ;; -*) ;; *) svcs+=("$1") ;; esac; shift; done
        for s in "${svcs[@]}"; do
          echo "up $s" >>"$st/events"
          echo "$s $(sed -n 's/^API_IMAGE=//p' "$SELFHOST_ENV_FILE")" >>"$st/ups"
          for n in $(list); do
            [[ $(f "$n" service) == "$s" ]] || continue
            if [[ $(f "$n" state) == restarting || $(f "$n" health) == unhealthy ]]; then echo "container $n is unhealthy" >&2; exit 1; fi
          done
          if [[ $s == api || ($s == engine && -n $scale) ]]; then
            have=$(ls "$st/c" | grep -c "^$s-" || true)
            for ((i = have; i < ${scale:-2}; i++)); do
              if [[ $s == api && -n ${NEW_API_BAD:-} ]]; then mk api restarting 1 '' new; echo "container api is unhealthy" >&2; exit 1; fi
              if [[ $s == engine && -n ${NEW_ENGINE_BAD:-} ]]; then mk engine restarting 1 '' new; echo "container engine is unhealthy" >&2; exit 1; fi
              mk "$s" running 0 healthy new
            done
          elif [[ $s == "${FAIL_SERVICE:-}" ]]; then mk "$s" restarting 1 '' new; echo "container $s is unhealthy" >&2; exit 1
          elif ! ls "$st/c" | grep -q "^$s-"; then mk "$s" running 0 healthy new
          fi
        done ;;
    esac ;;
esac
STUB
chmod +x "$fake/bin/docker"
fake_deploy() { # populates the stack from "service:state:exit:health ..." and runs deploy.sh; sets rc, out
  rm -rf "$fake/state"; mkdir -p "$fake/state/c"
  local spec
  # shellcheck disable=SC2034 # lib.sh reads $st
  local st=$fake/state sv state x h
  # shellcheck disable=SC1091
  . "$fake/lib.sh"
  for spec in $1; do IFS=: read -r sv state x h <<<"$spec"; mk "$sv" "$state" "$x" "$h" old; done
  printf 'API_IMAGE=old-api\nIMAGE_TAG=old\n' >"$fake/env"
  rc=0
  out=$(FAKE_STATE=$fake/state PATH="$fake/bin:$PATH" DEPLOY_PLAN="$fake/plan-ok.sh" SELFHOST_ENV_FILE="$fake/env" \
    bash "$fake/deploy/deploy.sh" sha-abcdef1 "${good[@]}" 2>&1 </dev/null) || rc=$?
}
events() { cat "$fake/state/events" 2>/dev/null || true; }
healthy_api() { grep -l '^health=healthy' "$fake"/state/c/api-* 2>/dev/null | wc -l; }
check() { # NAME condition-result
  if [[ $2 == 1 ]]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s (rc=%s)\n%s\n--- events\n%s\n' "$1" "$rc" "$out" "$(events)"; failures=$((failures + 1)); fi
}

# Leftover crash loops from earlier failed deploys: api-1..3 restart forever, api-4/5 are healthy, and the
# data services (one of them in an error state) must never be touched.
fake_deploy "api:restarting:1: api:restarting:1: api:restarting:1: api:running:0:healthy api:running:0:healthy postgres:running:0:healthy redis:running:0:healthy walship:exited:1: prometheus:running:0:healthy"
ev=$(events)
check "leftover crash-looping api containers do not fail the next deploy" "$((rc == 0))"
check "leftover crash-looping api containers are removed before health is judged" "$([[ -f $fake/state/c/api-1 || -f $fake/state/c/api-2 || -f $fake/state/c/api-3 ]] && echo 0 || echo 1)"
check "after the deploy two healthy api containers serve" "$(($(healthy_api) == 2))"
check "worker and proxy are started after the api converged" "$(grep -q '^up worker$' <<<"$ev" && grep -q '^up proxy$' <<<"$ev" && echo 1 || echo 0)"
check "data services are never removed or stopped" "$(grep -E '^(rm|stop) (postgres|redis|walship|prometheus|node-exporter)' <<<"$ev" | grep -q . && echo 0 || echo 1)"
check "no volume is removed (docker rm never gets -v/--volumes)" "$(grep -E '^(rm|stop)' "$fake/state/calls" >/dev/null; grep -E '^rm .*(-v|--volumes)' "$fake/state/calls" | grep -q . && echo 0 || echo 1)"

# A rollout whose new container never becomes healthy: roll back by removing the new container; the old
# generation keeps serving (never stopped), the script fails, and .env points at the old images again.
NEW_API_BAD=1 fake_deploy "api:running:0:healthy api:running:0:healthy postgres:running:0:healthy"
ev=$(events)
check "a failed rollout fails the deploy" "$((rc != 0))"
check "a failed rollout removes the new unhealthy container" "$([[ -f $fake/state/c/api-3 ]] && echo 0 || echo 1)"
check "a failed rollout leaves the previous healthy generation serving" "$(($(healthy_api) == 2))"
check "a failed rollout never stops an old container" "$(grep -q '^stop ' <<<"$ev" && echo 0 || echo 1)"
check "a failed rollout does not start worker or proxy" "$(grep -qE '^up (worker|proxy)$' <<<"$ev" && echo 0 || echo 1)"
check "a failed rollout restores the previous image references in .env" "$(grep -q '^API_IMAGE=old-api$' "$fake/env" && echo 1 || echo 0)"
# The next deploy (healthy this time) starts from that state and converges.
rc=0
out=$(FAKE_STATE=$fake/state PATH="$fake/bin:$PATH" DEPLOY_PLAN="$fake/plan-ok.sh" SELFHOST_ENV_FILE="$fake/env" bash "$fake/deploy/deploy.sh" sha-abcdef1 "${good[@]}" 2>&1 </dev/null) || rc=$?
check "the deploy after a failed rollout converges" "$((rc == 0 && $(healthy_api) == 2))"

# Zero-downtime ordering: with a running engine, the new engine is healthy before the old one drains (docker stop
# with the drain timeout, not the 10 s Docker default), then each API instance likewise, then worker and proxy.
fake_deploy "engine:running:0:healthy api:running:0:healthy api:running:0:healthy postgres:running:0:healthy"
ev=$(events)
line() { grep -n -m1 -- "$1" <<<"$ev" | cut -d: -f1; }
check "a rolling deploy with a running engine succeeds" "$((rc == 0))"
check "the new engine starts before the old engine is stopped" "$(( $(grep -c '^engine-' <(ls "$fake/state/c")) == 1 && $(line '^up engine$') < $(line '^stop engine-1$') ))"
check "the old engine is stopped and removed" "$([[ ! -f $fake/state/c/engine-1 && -f $fake/state/c/engine-2 ]] && echo 1 || echo 0)"
check "the engine rolls before the API instances" "$(( $(line '^stop engine-1$') < $(line '^stop api-1$') ))"
check "every old container gets the drain timeout, not Docker's 10 s default" "$([[ $(grep -c '^stop --timeout 120 ' "$fake/state/calls") == 3 ]] && echo 1 || echo 0)"
check "worker and proxy start after the API rolled" "$(( $(line '^stop api-2$') < $(line '^up worker$') ))"

# A new engine that never becomes healthy fails the deploy and leaves the old engine serving, never stopped.
NEW_ENGINE_BAD=1 fake_deploy "engine:running:0:healthy api:running:0:healthy api:running:0:healthy postgres:running:0:healthy"
check "an unhealthy new engine fails the deploy" "$((rc != 0))"
check "an unhealthy new engine leaves the old engine running and unstopped" "$([[ -f $fake/state/c/engine-1 ]] && ! grep -q '^stop engine' <(events) && echo 1 || echo 0)"

# The worker crash-looping (the staging TLS failure) fails the deploy but leaves no crash loop behind.
FAIL_SERVICE=worker fake_deploy "api:running:0:healthy api:running:0:healthy postgres:running:0:healthy"
check "a crash-looping worker fails the deploy" "$((rc != 0))"
check "a crash-looping worker is removed, not left restarting" "$(compgen -G "$fake/state/c/worker-*" >/dev/null && echo 0 || echo 1)"

# The smoke test fails after the whole new generation is running (the staging failure: smoke cannot reach the
# proxy). The rollback must bring engine, API, worker and proxy back to the previous image references, not only
# rewrite .env, and the deploy still fails.
FAKE_SMOKE_RC=1 fake_deploy "api:running:0:healthy api:running:0:healthy postgres:running:0:healthy"
ups=$(cat "$fake/state/ups" 2>/dev/null || true)
check "a failed smoke test fails the deploy" "$((rc != 0))"
check "the new generation was rolled out before the smoke test" "$(grep -q '^worker ghcr' <<<"$ups" && grep -q '^proxy ghcr' <<<"$ups" && echo 1 || echo 0)"
check "a failed smoke test restores the previous image references in .env" "$(grep -q '^API_IMAGE=old-api$' "$fake/env" && echo 1 || echo 0)"
check "a failed smoke test restarts engine, worker and proxy on the previous images" "$(for s in engine worker proxy; do grep "^$s " <<<"$ups" | tail -n 1; done | grep -c ' old-api$' | grep -qx 3 && echo 1 || echo 0)"
check "a failed smoke test leaves two healthy api containers" "$(($(healthy_api) == 2))"
check "a failed smoke test never removes a data service" "$(grep -E '^(rm|stop) (postgres|redis|walship|prometheus|node-exporter)' <<<"$(events)" | grep -q . && echo 0 || echo 1)"

((failures == 0)) || { printf '%s failure(s)\n' "$failures" >&2; exit 1; }
