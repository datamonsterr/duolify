#!/usr/bin/env bash
# Deploy on the VM (run by .github/workflows/deploy-vm.yml over SSH, or by hand):
#   deploy.sh sha-<commit>
# Needs infra/deploy/.env (from `SELFHOST_ENV_TEMPLATE=.env.prod.example selfhost.sh init`) and the
# repository's infra/ directory next to this script. Steps: pull the GHCR images for the tag and log
# their digests, apply migrations with `/api migrate`, then restart the engine, the API instances
# one at a time (a new instance is healthy before an old one stops), the worker and the proxy.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
tag=${1:?usage: deploy.sh sha-<commit>}
[[ $tag =~ ^sha-[0-9a-f]{7,40}$ ]] || { echo "deploy: refusing tag '$tag' (want sha-<commit>)" >&2; exit 2; }
export SELFHOST_OVERLAY=$here/compose.prod.yml
env_file=${SELFHOST_ENV_FILE:-$here/.env}
compose() { bash "$here/selfhost.sh" compose "$@"; }
say() { printf '==> %s\n' "$*"; }

[[ -f $env_file ]] || { echo "deploy: missing $env_file; run selfhost.sh init first" >&2; exit 1; }
if grep -q '^IMAGE_TAG=' "$env_file"; then sed -i "s|^IMAGE_TAG=.*|IMAGE_TAG=$tag|" "$env_file"; else echo "IMAGE_TAG=$tag" >>"$env_file"; fi

say "pulling images for $tag"
compose pull --ignore-buildable api worker engine proxy backup-init
for image in api worker engine web; do
  docker image inspect --format '{{index .RepoDigests 0}}' "$(grep '^IMAGE_REGISTRY=' "$env_file" | cut -d= -f2-)/$image:$tag" || true
done
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
