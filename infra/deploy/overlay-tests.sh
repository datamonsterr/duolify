#!/usr/bin/env bash
# Tests for the production overlay (compose.prod.yml) against a throwaway local stack.
#
#   overlay-tests.sh e2e    stack up with the overlay: 2 API instances, private Engine, WAL reaches the bucket
#   overlay-tests.sh pitr   PITR drill: base backup + archived WAL restored to a point in time
#   overlay-tests.sh redis  Redis cold start: flush Redis, stack still serves, state rebuilds from Postgres
#   overlay-tests.sh dr     nightly DR copy keeps every object version, even after a source delete
#   overlay-tests.sh rolling    kill each API instance in turn under steady GET load: no failed request through the proxy
#   overlay-tests.sh lifecycle  backup-lifecycle.sh apply/verify against the RustFS backup bucket
#
# Runs under its own Compose project and ports (never the owner's stack); removes its containers and
# volumes on exit. Needs Docker and the duolify/*:local images (built on demand).
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
work=$(mktemp -d)
chmod 700 "$work"
suffix=$(head -c 4 /dev/urandom | od -An -vtx1 | tr -d ' \n')
export SELFHOST_PROJECT=duolify-e18-08-$suffix
export SELFHOST_ENV_FILE=$work/env
export SELFHOST_AGE_IDENTITY_FILE=$work/age-identity.txt
export SELFHOST_OVERLAY=$here/compose.prod.yml
sh=$here/selfhost.sh

compose() { bash "$sh" compose "$@"; }
say() { printf '==> %s\n' "$*"; }
die() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
port() { printf '%s' $((20000 + RANDOM % 20000)); }

cleanup() {
  compose --profile '*' down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

start_stack() {
  (umask 077 && cp "$here/.env.example" "$SELFHOST_ENV_FILE")
  {
    printf 'BIND_ADDRESS=127.0.0.1\nHTTP_PORT=%s\nHTTPS_PORT=%s\n' "$(port)" "$(port)"
    printf 'COMPOSE_PROFILES=local-s3\n'
  } >>"$SELFHOST_ENV_FILE"
  sed -i "s|^PUBLIC_ORIGIN=.*|PUBLIC_ORIGIN=http://127.0.0.1|" "$SELFHOST_ENV_FILE"
  bash "$sh" up
}

# aws CLI with the RustFS root identity (test-only: inspects versions, which scoped keys cannot).
root_aws() {
  compose run --rm -T --no-deps --entrypoint aws s3-init "$@"
}

wait_for() { # SECONDS DESCRIPTION COMMAND...
  local secs=$1 what=$2 i
  shift 2
  for ((i = 0; i < secs; i += 3)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 3
  done
  die "timed out waiting for $what"
}

wal_in_bucket() { # at least one archived WAL segment in the backup bucket
  compose run --rm -T --no-deps backup-reader s3api list-objects-v2 --bucket duolify-backups \
    --prefix wal/ --query 'length(Contents)' --output text | grep -qE '^[1-9]'
}

test_e2e() {
  start_stack
  bash "$sh" smoke
  local n
  n=$(compose ps --format '{{.Health}}' api | grep -c '^healthy$')
  [[ $n == 2 ]] || die "expected 2 healthy API instances, got $n"
  [[ $(compose config --format json | jq -r '.services.engine.ports // [] | length') == 0 ]] || die "engine publishes a port"
  compose exec -T postgres psql -U duolify -d duolify -Atc 'select pg_switch_wal()' >/dev/null
  wait_for 90 "WAL segment in the backup bucket" wal_in_bucket
  [[ $(compose exec -T postgres psql -U duolify -d duolify -Atc 'select failed_count from pg_stat_archiver') == 0 ]] || die "archiver reports failures"
  say "e2e passed"
}

test_pitr() {
  start_stack
  compose exec -T postgres psql -U duolify -d duolify -qc 'create table pitr_marker(label text)' -c "insert into pitr_marker values ('before')"
  compose exec -T walship /walship.sh base
  compose exec -T postgres psql -U duolify -d duolify -qc "insert into pitr_marker values ('kept')"
  sleep 2
  local target
  target=$(compose exec -T postgres psql -U duolify -d duolify -Atc "select to_char(now() at time zone 'UTC','YYYY-MM-DD HH24:MI:SS')||'+00'")
  sleep 2
  compose exec -T postgres psql -U duolify -d duolify -qc "insert into pitr_marker values ('after')" -c 'select pg_switch_wal()'
  wait_for 90 "WAL segment in the backup bucket" wal_in_bucket
  sleep 15 # let the shipper pick up the segment holding 'after'
  local out
  out=$(bash "$sh" pitr-drill "$target")
  printf '%s\n' "$out"
  grep -qx 'pitr_labels=before,kept' <<<"$out" || die "PITR did not stop at the target time"
  say "pitr passed"
}

test_redis() {
  start_stack
  compose exec -T redis redis-cli SET duolify:e18-08 1 >/dev/null
  compose exec -T redis redis-cli FLUSHALL >/dev/null
  compose restart redis >/dev/null
  wait_for 60 "redis healthy" bash -c "[[ \$(bash '$sh' compose ps --format '{{.Health}}' redis) == healthy ]]"
  compose exec -T redis redis-cli --raw EXISTS duolify:e18-08 | grep -qx 0 || die "redis kept state across the flush"
  bash "$sh" smoke
  say "redis passed"
}

test_dr() {
  start_stack
  local key=lessons/e18-08/dr-probe.txt dr=dr/lesson-assets/lessons/e18-08/dr-probe.txt
  printf v1 >"$work/obj"
  put() { root_aws s3 cp - "s3://duolify/$key" <"$work/obj"; }
  put
  compose exec -T walship /walship.sh dr-copy
  printf v2 >"$work/obj"; put
  compose exec -T walship /walship.sh dr-copy
  root_aws s3 rm "s3://duolify/$key"
  compose exec -T walship /walship.sh dr-copy
  local versions marker
  versions=$(root_aws s3api list-object-versions --bucket duolify-backups --prefix "$dr" --query 'length(Versions)' --output text)
  # shellcheck disable=SC2016 # JMESPath backticks are literals
  marker=$(root_aws s3api list-object-versions --bucket duolify-backups --prefix "$dr" --query 'length(DeleteMarkers || `[]`)' --output text)
  [[ $versions == 2 ]] || die "expected 2 DR versions, got $versions"
  [[ $marker == 0 ]] || die "DR copy propagated the source delete"
  say "dr passed"
}

# backup-lifecycle.sh runs inside the walship image (aws CLI) on the stack network, with the RustFS root key.
lifecycle_sh() {
  local envf=$work/lifecycle.env
  (umask 077 && {
    printf 'BACKUP_S3_ENDPOINT=http://objectstore:9000\nBACKUP_S3_REGION=us-east-1\nBACKUP_S3_BUCKET=duolify-backups\n'
    printf 'BACKUP_ADMIN_S3_ACCESS_KEY_ID=duolify-admin\nBACKUP_ADMIN_S3_SECRET_ACCESS_KEY=%s\n' "$(sed -n 's/^S3_ROOT_SECRET_ACCESS_KEY=//p' "$SELFHOST_ENV_FILE" | tail -n 1)"
  } >"$envf")
  compose run --rm -T --no-deps --entrypoint bash --env-from-file "$envf" --volume "$here/backup-lifecycle.sh:/lc.sh:ro" walship /lc.sh "$@"
}

test_lifecycle() {
  start_stack
  if lifecycle_sh verify >"$work/o" 2>&1; then cat "$work/o"; die "verify passed before any lifecycle rule was applied"; fi
  grep -q 'no enabled lifecycle rule' "$work/o" || { cat "$work/o"; die "verify did not report the missing rule"; }
  lifecycle_sh apply
  lifecycle_sh verify
  if lifecycle_sh verify --require-base >"$work/o" 2>&1; then die "verify --require-base passed with no base backup"; fi
  grep -q 'no base backup' "$work/o" || { cat "$work/o"; die "verify did not report the missing base"; }
  compose exec -T walship /walship.sh base
  lifecycle_sh verify --require-base
  # A rule that expires wal/ no later than base/ must be rejected.
  root_aws s3api put-bucket-lifecycle-configuration --bucket duolify-backups --lifecycle-configuration \
    '{"Rules":[{"ID":"b","Status":"Enabled","Filter":{"Prefix":"base/"},"Expiration":{"Days":9},"NoncurrentVersionExpiration":{"NoncurrentDays":1}},{"ID":"w","Status":"Enabled","Filter":{"Prefix":"wal/"},"Expiration":{"Days":9},"NoncurrentVersionExpiration":{"NoncurrentDays":1}}]}'
  if lifecycle_sh verify >"$work/o" 2>&1; then die "verify accepted wal/ expiring no later than base/"; fi
  grep -q 'must expire later' "$work/o" || { cat "$work/o"; die "verify did not explain the wal/base ordering"; }
  say "lifecycle passed"
}

test_rolling() {
  start_stack
  local url n=0 bad=0 ok=0 code id pid pid2
  url="http://127.0.0.1:$(sed -n 's/^HTTP_PORT=//p' "$SELFHOST_ENV_FILE" | tail -n 1)/v1/features"
  curl -fsS "$url" >/dev/null || die "baseline request failed"
  : >"$work/codes"
  touch "$work/load"
  # Two clients: GET and POST (a POST answered with 4xx is fine; a proxy error 502/503/504 or no answer is not).
  (while [[ -e $work/load ]]; do curl -s -o /dev/null -w '%{http_code}\n' --max-time 10 "$url" >>"$work/codes" || echo 000 >>"$work/codes"; sleep 0.02; done) &
  pid=$!
  (while [[ -e $work/load ]]; do curl -s -o /dev/null -w '%{http_code}\n' --max-time 10 -X POST -d '{}' -H 'content-type: application/json' "$url" >>"$work/codes" || echo 000 >>"$work/codes"; sleep 0.02; done) &
  pid2=$!
  sleep 2
  for id in $(compose ps --quiet api); do
    # SIGKILL (worst case: no graceful drain): the proxy must retry onto the surviving instance.
    docker kill "$id" >/dev/null
    sleep 4
    docker start "$id" >/dev/null
    wait_for 90 "api instance back" bash -c "[[ \$(docker inspect --format '{{.State.Health.Status}}' $id) == healthy ]]"
    sleep 2
  done
  rm -f "$work/load"
  wait "$pid" "$pid2"
  while read -r code; do n=$((n + 1)); if [[ $code =~ ^[1-4][0-9][0-9]$ ]]; then ok=$((ok + 1)); else bad=$((bad + 1)); fi; done <"$work/codes"
  printf 'rolling: %s requests, %s ok, %s failed\n' "$n" "$ok" "$bad"
  sort "$work/codes" | uniq -c
  ((n > 100)) || die "load generator produced too few requests"
  ((bad == 0)) || die "$bad request(s) failed while API instances were stopped"
  say "rolling passed"
}

case ${1:-} in
  rolling) test_rolling ;;
  lifecycle) test_lifecycle ;;
  e2e) test_e2e ;;
  pitr) test_pitr ;;
  redis) test_redis ;;
  dr) test_dr ;;
  *) sed -n '2,9p' "$0" >&2; exit 2 ;;
esac
