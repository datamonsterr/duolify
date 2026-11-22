#!/usr/bin/env bash
# Deploy on the VM (run by .github/workflows/deploy-vm.yml over SSH, or by hand):
#   deploy.sh sha-<commit> api=<ref> worker=<ref> engine=<ref> web=<ref> backup-init=<ref>
# Every <ref> must be a digest-pinned image, <repo>@sha256:<64 hex>, the digest published by the image
# build for that commit; a tag (even sha-<commit>) is refused, so a re-pushed tag cannot change what runs.
# Needs infra/deploy/.env (from `SELFHOST_ENV_TEMPLATE=.env.prod.example selfhost.sh init`) and the
# repository's infra/ directory next to this script. Steps: record the refs in .env (API_IMAGE ...),
# pull them, apply migrations with `/api migrate`, then restart the engine, the API instances one at a
# time (a new instance is healthy before an old one stops), the worker and the proxy.
# Before anything is pulled it runs deploy-plan.sh against the env file (fail fast on bad settings).
# See docs/infra/hardening.md "Deploys" for the migration and restart-order rules and failure states.
# Convergence: a failed deploy never leaves a crash loop behind. Before the data services are judged, containers
# of api, worker, engine and proxy that are restarting, dead, exited with an error or unhealthy (leftovers of an
# earlier failed deploy) are removed; if the rolling restart fails, the new API containers that never became
# healthy are removed (the old generation, which is only stopped after its replacement is healthy, keeps serving),
# the env file gets its previous image references back, and any broken container is removed again. Data services
# (postgres, redis, walship, prometheus, node-exporter) and volumes are never touched.
# If the smoke test fails after the rollout, the whole stack is rolled back too: the env file gets its previous
# image references and engine, API, worker and proxy are restarted on them, so what runs matches .env.
# Private images: DEPLOY_REGISTRY_USER=<login> logs Docker in to ghcr.io with a token read from stdin
# (deploy-vm passes the job's short-lived GITHUB_TOKEN, `packages: read`). The login lives in a throwaway
# DOCKER_CONFIG and is logged out and deleted right after the pull and on any exit; the token never
# appears on a command line or in ~/.docker.
# DEPLOY_VALIDATE_ONLY=1 checks the arguments (and the registry token, when a user is set) and exits (tests).
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
die() { echo "deploy: $*" >&2; exit 2; }
tag=${1:-}
[[ $tag =~ ^sha-[0-9a-f]{7,40}$ ]] || die "refusing tag '$tag' (usage: deploy.sh sha-<commit> api=<repo>@sha256:<digest> worker=... engine=... web=... backup-init=...)"
shift
declare -A pinned=()
for arg in "$@"; do
  name=${arg%%=*}
  ref=${arg#*=}
  case $name in api | worker | engine | web | backup-init) ;; *) die "unknown image name '$name' (want api worker engine web backup-init)" ;; esac
  [[ $ref =~ ^[a-z0-9][a-z0-9._/-]*@sha256:[0-9a-f]{64}$ ]] || die "image $name: '$ref' is not <repo>@sha256:<64 hex>; deploys are by digest, tags are refused"
  pinned[$name]=$ref
done
for name in api worker engine web backup-init; do
  [[ -n ${pinned[$name]:-} ]] || die "no digest for image $name; pass $name=<repo>@sha256:<digest> (a deploy needs all five, and only digests)"
done
registry_user=${DEPLOY_REGISTRY_USER:-}
registry_token=''
if [[ -n $registry_user ]]; then
  [[ $registry_user =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,38}(\[bot\])?$ ]] || die "DEPLOY_REGISTRY_USER '$registry_user' is not a GitHub login"
  IFS= read -r registry_token || true
  [[ -n $registry_token ]] || die "DEPLOY_REGISTRY_USER is set but no registry token arrived on stdin"
fi
[[ -z ${DEPLOY_VALIDATE_ONLY:-} ]] || exit 0

export SELFHOST_OVERLAY=$here/compose.prod.yml
env_file=${SELFHOST_ENV_FILE:-$here/.env}
compose() { bash "$here/selfhost.sh" compose "$@"; }
say() { printf '==> %s\n' "$*"; }

[[ -f $env_file ]] || { echo "deploy: missing $env_file; run selfhost.sh init first" >&2; exit 1; }

# Fail before pulling or migrating on settings the stack would only reject at start (a backup key equal
# to the app key, a missing or malformed age recipient, a localhost asset origin, oversized limits ...).
# Needs jq and `docker compose` on the VM. DEPLOY_PLAN is a test seam.
say "checking $env_file (deploy-plan)"
command -v jq >/dev/null || { echo "deploy: jq is required for the pre-deploy check (apt install jq)" >&2; exit 1; }
DEPLOY_PLAN_ENV=$env_file bash "${DEPLOY_PLAN:-$here/deploy-plan.sh}" "$here" ||
  { echo "deploy: $env_file failed deploy-plan; nothing was pulled or migrated. Fix it and redeploy." >&2; exit 1; }

env_backup=$(mktemp)
cp -p "$env_file" "$env_backup"
setenv() { if grep -q "^$1=" "$env_file"; then sed -i "s|^$1=.*|$1=$2|" "$env_file"; else echo "$1=$2" >>"$env_file"; fi; }
setenv IMAGE_TAG "$tag" # only the locally built walship image uses the tag
setenv API_IMAGE "${pinned[api]}"
setenv WORKER_IMAGE "${pinned[worker]}"
setenv ENGINE_IMAGE "${pinned[engine]}"
setenv WEB_IMAGE "${pinned[web]}"
setenv BACKUP_INIT_IMAGE "${pinned[backup-init]}"

# Containers of the stack as "<id>|<service>|<state>|<exit code>|<health>" (ids are short).
stack_containers() { compose ps --all --format '{{.ID}}|{{.Service}}|{{.State}}|{{.ExitCode}}|{{.Health}}' 2>/dev/null || true; }
# Only these services may be removed here; never a data service.
removable=' api worker engine proxy '
remove_broken() {
  local id svc state code health
  while IFS='|' read -r id svc state code health; do
    [[ $removable == *" $svc "* ]] || continue
    if [[ $state == restarting || $state == dead || ($state == exited && $code != 0) || $health == unhealthy ]]; then
      say "removing broken $svc container $id (state $state, exit $code, health ${health:-none})"
      docker rm --force "$id" >/dev/null 2>&1 || true # no -v: volumes stay
    fi
  done < <(stack_containers)
}
# Remove API containers that are not part of the previous generation and are not healthy.
rollback_api() {
  local id svc state code health o known
  while IFS='|' read -r id svc state code health; do
    [[ $svc == api ]] || continue
    known=0
    for o in "${old[@]}"; do [[ $o == "$id"* || $id == "$o"* ]] && known=1; done
    ((known)) && continue
    [[ $health == healthy ]] && continue
    say "rolling back: removing new api container $id (state $state, health ${health:-none})"
    docker rm --force "$id" >/dev/null 2>&1 || true
  done < <(stack_containers)
}
phase=start
old=()
registry_logout() {
  [[ -n ${DOCKER_CONFIG:-} && -d $DOCKER_CONFIG ]] || return 0
  docker logout ghcr.io >/dev/null 2>&1 || true
  rm -rf "$DOCKER_CONFIG"
  unset DOCKER_CONFIG
}
if [[ -n $registry_user ]]; then
  DOCKER_CONFIG=$(mktemp -d)
  export DOCKER_CONFIG
  say "registry login ghcr.io as $registry_user (temporary)"
  printf '%s\n' "$registry_token" | docker login ghcr.io --username "$registry_user" --password-stdin >/dev/null
  registry_token=''
fi

on_exit() {
  local rc=$?
  registry_logout
  if ((rc != 0)) && [[ $phase == converge || $phase == verify ]]; then
    local failed_phase=rollout
    [[ $phase == verify ]] && failed_phase=verify
    say "deploy failed (rc=$rc): rolling back"
    [[ $failed_phase == rollout ]] && rollback_api
    remove_broken
    cp -p "$env_backup" "$env_file"
    if [[ $failed_phase == rollout ]]; then
      echo "deploy: rolled back; the previous healthy containers keep serving, $env_file has its previous image references" >&2
    elif [[ -n $(setting_of API_IMAGE) ]]; then
      # The new generation was already running (it failed the smoke test): bring the containers back to the
      # previous image references so what runs matches .env. Never touches data services or volumes.
      say "smoke failed after the rollout: restarting the previous generation"
      phase=rollback
      if (roll_all); then echo "deploy: rolled back to the previous image references; $env_file matches the running containers" >&2
      else echo "deploy: ROLLBACK FAILED; containers may run a mix of generations, check 'docker ps' against $env_file" >&2; fi
    else
      echo "deploy: smoke failed and $env_file has no previous image references to return to; the new generation keeps running" >&2
    fi
  fi
  rm -f "$env_backup"
  return "$rc"
}
trap on_exit EXIT

say "pulling digest-pinned images for $tag"
compose pull --ignore-buildable api worker engine proxy backup-init
registry_logout
for name in api worker engine web backup-init; do echo "  $name ${pinned[$name]}"; done
compose build walship

phase=converge
say "removing leftovers of earlier failed deploys"
remove_broken

say "data services (created if missing, never recreated here)"
compose up --detach --wait --no-recreate postgres redis walship node-exporter prometheus

say "/api migrate"
compose run --rm --no-deps migrate

roll_api() { # one new API instance is healthy before an old one stops
  mapfile -t old < <(compose ps --quiet api)
  local count=${#old[@]} id
  ((count > 0)) || count=2
  for id in "${old[@]}"; do
    compose up --detach --wait --no-deps --no-recreate --scale "api=$((count + 1))" api
    docker stop "$id" >/dev/null && docker rm "$id" >/dev/null
  done
  compose up --detach --wait --no-deps --no-recreate --scale "api=$count" api
}
roll_all() {
  say "engine"
  compose up --detach --wait --no-deps engine
  say "rolling restart: API"
  roll_api
  say "worker and proxy"
  compose up --detach --wait --no-deps worker proxy
}
setting_of() { sed -n "s/^$1=//p" "$env_file" | tail -n 1; }

roll_all
phase=verify
bash "$here/selfhost.sh" smoke
phase=finished
say "deployed $tag"
