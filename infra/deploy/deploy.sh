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

setenv() { if grep -q "^$1=" "$env_file"; then sed -i "s|^$1=.*|$1=$2|" "$env_file"; else echo "$1=$2" >>"$env_file"; fi; }
setenv IMAGE_TAG "$tag" # only the locally built walship image uses the tag
setenv API_IMAGE "${pinned[api]}"
setenv WORKER_IMAGE "${pinned[worker]}"
setenv ENGINE_IMAGE "${pinned[engine]}"
setenv WEB_IMAGE "${pinned[web]}"
setenv BACKUP_INIT_IMAGE "${pinned[backup-init]}"

registry_logout() {
  [[ -n ${DOCKER_CONFIG:-} && -d $DOCKER_CONFIG ]] || return 0
  docker logout ghcr.io >/dev/null 2>&1 || true
  rm -rf "$DOCKER_CONFIG"
  unset DOCKER_CONFIG
}
if [[ -n $registry_user ]]; then
  DOCKER_CONFIG=$(mktemp -d)
  export DOCKER_CONFIG
  trap registry_logout EXIT
  say "registry login ghcr.io as $registry_user (temporary)"
  printf '%s\n' "$registry_token" | docker login ghcr.io --username "$registry_user" --password-stdin >/dev/null
  registry_token=''
fi

say "pulling digest-pinned images for $tag"
compose pull --ignore-buildable api worker engine proxy backup-init
registry_logout
for name in api worker engine web backup-init; do echo "  $name ${pinned[$name]}"; done
compose build walship

say "data services (created if missing, never recreated here)"
compose up --detach --wait --no-recreate postgres redis walship node-exporter prometheus

say "/api migrate"
compose run --rm --no-deps migrate

say "engine"
compose up --detach --wait --no-deps engine

say "rolling restart: API"
mapfile -t old < <(compose ps --quiet api)
count=${#old[@]}
((count > 0)) || count=2
for id in "${old[@]}"; do
  compose up --detach --wait --no-deps --no-recreate --scale "api=$((count + 1))" api
  docker stop "$id" >/dev/null && docker rm "$id" >/dev/null
done
compose up --detach --wait --no-deps --no-recreate --scale "api=$count" api

say "worker and proxy"
compose up --detach --wait --no-deps worker proxy
bash "$here/selfhost.sh" smoke
say "deployed $tag"
