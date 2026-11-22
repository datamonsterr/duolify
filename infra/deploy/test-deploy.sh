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
PATH="$tmp/bin:$PATH" DEPLOY_REGISTRY_USER=octocat bash "$tmp/deploy/deploy.sh" sha-abcdef1 "${good[@]}" <"$tmp/token" >/dev/null 2>&1 || rc=$?
calls=$(cat "$tmp/calls" 2>/dev/null || true)
config_dir=$(sed -n 's/^argv login .* config=//p' <<<"$calls")
if ((rc != 0)) && grep -q '^stdin ghs_faketoken$' <<<"$calls" && grep -q '^logout$' <<<"$calls" \
  && ! grep '^argv' <<<"$calls" | grep -q ghs_faketoken && [[ -n $config_dir && $config_dir != none && ! -e $config_dir ]]; then
  printf 'ok   registry login via stdin in a temporary config, logged out after a failed pull\n'
else printf 'FAIL registry login/logout (rc=%s):\n%s\n' "$rc" "$calls"; failures=$((failures + 1)); fi

# The Compose files must honour the pinned references.
out=$(API_IMAGE=ghcr.io/o/r/api@sha256:$d1 WORKER_IMAGE=ghcr.io/o/r/worker@sha256:$d1 ENGINE_IMAGE=ghcr.io/o/r/engine@sha256:$d1 \
  WEB_IMAGE=ghcr.io/o/r/web@sha256:$d1 BACKUP_INIT_IMAGE=ghcr.io/o/r/backup-init@sha256:$d1 \
  docker compose --project-directory "$here" --file "$here/compose.yml" --file "$here/compose.prod.yml" --env-file "$here/.env.prod.example" --profile '*' config --format json \
  | jq -r '[.services.api, .services.migrate, .services.worker, .services.engine, .services.proxy, .services["backup-init"]] | map(.image) | join(" ")')
if [[ $(tr ' ' '\n' <<<"$out" | grep -c "@sha256:$d1\$") == 6 ]]; then printf 'ok   compose uses the pinned references\n'
else printf 'FAIL compose did not use the pinned references: %s\n' "$out"; failures=$((failures + 1)); fi

((failures == 0)) || { printf '%s failure(s)\n' "$failures" >&2; exit 1; }
