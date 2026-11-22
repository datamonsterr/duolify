#!/usr/bin/env bash
# Tests for the production overlay (compose.prod.yml) against a throwaway local stack.
#
#   overlay-tests.sh e2e    stack up with the overlay: 2 API instances, private Engine, WAL reaches the bucket
#   overlay-tests.sh pitr   PITR drill: base backup + archived WAL restored to a point in time
#   overlay-tests.sh redis  Redis cold start: flush Redis, stack still serves, state rebuilds from Postgres
#   overlay-tests.sh dr     nightly DR copy keeps every object version, even after a source delete
#   overlay-tests.sh ca     api, worker, engine and walship images carry a CA bundle (outbound HTTPS verifies)
#   overlay-tests.sh rolling    stop each API instance in turn (docker stop, as deploy.sh does) under GET and POST load plus slow in-flight requests: no failed request
#   overlay-tests.sh sse       a stream silent for 10-15 s at a time lives >150 s through the proxy, unbuffered
#   overlay-tests.sh lifecycle  backup-lifecycle.sh apply/verify against the RustFS backup bucket
#   overlay-tests.sh retry     two replicas behind one name, one drops connections: every GET still answered (also runs after rolling)
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
port() { # a random port nothing listens on and not handed out before
  local p
  while :; do
    p=$((20000 + RANDOM % 20000))
    [[ " ${used_ports:-} " == *" $p "* ]] && continue
    ss -ltn 2>/dev/null | awk '{print $4}' | grep -q ":$p\$" && continue
    used_ports="${used_ports:-} $p"
    printf '%s' "$p"
    return
  done
}

cleanup() {
  docker rm -f "$SELFHOST_PROJECT-sse-proxy" "$SELFHOST_PROJECT-sse-stub" "$SELFHOST_PROJECT-retry-proxy" \
    "$SELFHOST_PROJECT-retry-good" "$SELFHOST_PROJECT-retry-bad" >/dev/null 2>&1 || true
  docker network rm "$SELFHOST_PROJECT-retry" >/dev/null 2>&1 || true
  compose --profile '*' down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

prepare_env() {
  (umask 077 && cp "$here/.env.example" "$SELFHOST_ENV_FILE")
  {
    printf 'BIND_ADDRESS=127.0.0.1\nHTTP_PORT=%s\nHTTPS_PORT=%s\n' "$(port)" "$(port)"
    printf 'COMPOSE_PROFILES=local-s3\n'
  } >>"$SELFHOST_ENV_FILE"
  sed -i "s|^PUBLIC_ORIGIN=.*|PUBLIC_ORIGIN=http://127.0.0.1|" "$SELFHOST_ENV_FILE"
}

start_stack() {
  prepare_env
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

# Every image that makes outbound HTTPS calls carries a CA bundle. CI and this stack talk plain HTTP to a local
# object store, so a missing bundle (the worker image on staging, Cloudflare R2 over HTTPS) is invisible to the
# other tests. Builds the images (cached after e2e) and inspects them without starting anything.
test_ca() {
  prepare_env
  local images
  compose --profile '*' build api worker engine walship
  mapfile -t images < <(compose --profile '*' config --format json | jq -r '[.services.api, .services.worker, .services.engine, .services.walship] | map(.image) | .[]')
  ((${#images[@]} == 4)) || die "expected 4 images, got ${#images[@]}"
  bash "$here/test-image-ca.sh" "${images[@]}" || die "an image has no usable CA bundle"
  say "ca passed"
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
  local base url post n=0 bad=0 ok=0 pn=0 pbad=0 sn=0 sbad=0 code id i pids=()
  base="http://127.0.0.1:$(sed -n 's/^HTTP_PORT=//p' "$SELFHOST_ENV_FILE" | tail -n 1)"
  url=$base/v1/features
  post=$base/v1/auth/logout # a real DB write, idempotent for an unknown token: 204 every time
  curl -fsS "$url" >/dev/null || die "baseline request failed"
  [[ $(curl -s -o /dev/null -w '%{http_code}' -X POST -d '{"refresh_token":"x"}' -H 'content-type: application/json' "$post") == 204 ]] || die "baseline POST is not 204"
  : >"$work/codes"
  touch "$work/load"
  # Fast GET and POST clients in parallel, no pause. Every one must get a 2xx while instances are stopped the way
  # deploy.sh stops them (docker stop: SIGTERM, drain, SIGKILL only after the grace period).
  for i in 1 2 3 4 5 6 7 8; do
    (while [[ -e $work/load ]]; do curl -s -o /dev/null -w '%{http_code}\n' --max-time 10 "$url" >>"$work/codes.$i" || echo 000 >>"$work/codes.$i"; done) &
    pids+=($!)
  done
  for i in 9 10 11 12; do
    (while [[ -e $work/load ]]; do curl -s -o /dev/null -w '%{http_code}\n' --max-time 10 -X POST -d '{"refresh_token":"x"}' -H 'content-type: application/json' "$post" >>"$work/post.$i" || echo 000 >>"$work/post.$i"; done) &
    pids+=($!)
  done
  sleep 2
  local spids=()
  slow_requests() { # six slow GETs (a 2 s stream) and six slow POSTs (body arrives over 3 s), all started now
    local k=$1 s
    for s in 1 2 3 4 5 6; do
      (code=$(curl -sS -o "$work/stream.$k.$s" -w '%{http_code}' --max-time 20 "$base/v1/stream-check") || code=000
        [[ $code == 200 && $(grep -c '^event: ping' "$work/stream.$k.$s") == 2 ]] && echo ok >>"$work/slow.$k" || echo "bad $code" >>"$work/slow.$k") &
      spids+=($!)
      (code=$( (printf '{"refresh_token":"'; sleep 3; printf 'x"}') | curl -s -o /dev/null -w '%{http_code}' --max-time 20 -X POST -H 'content-type: application/json' -H 'Transfer-Encoding: chunked' --data-binary @- "$post") || code=000
        [[ $code == 204 ]] && echo ok >>"$work/slow.$k" || echo "bad $code" >>"$work/slow.$k") &
      spids+=($!)
    done
  }
  k=0
  for id in $(compose ps --quiet api); do
    k=$((k + 1))
    slow_requests "$k"
    sleep 1 # the slow requests are now in flight on both instances
    if [[ -n ${ROLLING_NEGATIVE_CONTROL:-} ]]; then docker kill "$id" >/dev/null; else docker stop "$id" >/dev/null; fi # control: SIGKILL cuts in-flight requests, the test must then fail
    wait "${spids[@]}" # the slow requests started above (not the load loops, which run until $work/load is removed)
    spids=()
    sleep 1
    docker start "$id" >/dev/null
    wait_for 90 "api instance back" bash -c "[[ \$(docker inspect --format '{{.State.Health.Status}}' $id) == healthy ]]"
    sleep 2
  done
  rm -f "$work/load"
  wait "${pids[@]}"
  cat "$work"/codes.* >"$work/codes"
  cat "$work"/post.* >"$work/posts"
  cat "$work"/slow.* >"$work/slow"
  while read -r code; do n=$((n + 1)); [[ $code =~ ^2[0-9][0-9]$ ]] && ok=$((ok + 1)) || bad=$((bad + 1)); done <"$work/codes"
  while read -r code; do pn=$((pn + 1)); [[ $code =~ ^2[0-9][0-9]$ ]] || pbad=$((pbad + 1)); done <"$work/posts"
  while read -r code; do sn=$((sn + 1)); [[ $code == ok ]] || sbad=$((sbad + 1)); done <"$work/slow"
  printf 'rolling: GET %s requests, %s failed; POST %s requests, %s failed; slow in-flight (stream + slow POST) %s, %s failed\n' "$n" "$bad" "$pn" "$pbad" "$sn" "$sbad"
  sort "$work/codes" | uniq -c
  sort "$work/posts" | uniq -c
  sort "$work/slow" | uniq -c
  ((n > 100 && pn > 50)) || die "load generator produced too few requests"
  ((bad == 0)) || die "$bad GET(s) failed while API instances were stopped"
  ((pbad == 0)) || die "$pbad POST(s) failed while API instances were stopped"
  ((sbad == 0)) || die "$sbad slow in-flight request(s) were cut while an instance was stopped"
  say "rolling passed"
  test_retry
}

# SSE through the proxy. A stub upstream (busybox nc) sends 14 events with 10-15 s of silence between
# them (~165 s). The real Caddyfile is mounted into a caddy container on the stack's edge network and
# pointed at the stub with API_UPSTREAM, so the /v1 reverse_proxy options under test are the production
# ones, not a copy. (The stack's own proxy can only reach `api`; #264 heartbeats are not needed.)
# Each event carries the stub's send time and the client stamps its receive time. Both read the host
# kernel's /proc/uptime (shared with containers), so the delay is measured to 10 ms.
cs() { awk '{printf "%d", $1 * 100}' /proc/uptime; }

sse_stub_script() {
  cat <<'S'
#!/bin/sh
# nc -e entry: read the request head, then stream events with long silences.
while IFS= read -r l; do [ "$l" = "$(printf '\r')" ] && break; done
printf 'HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nCache-Control: no-cache\r\nConnection: close\r\n\r\n'
cs() { awk '{printf "%d", $1 * 100}' /proc/uptime; }
i=0
for gap in 10 15 12 14 11 13 15 10 12 14 11 13 15 12; do
  i=$((i + 1))
  printf 'data: {"seq":%s,"sent":%s}\n\n' "$i" "$(cs)"
  sleep "$gap"
done
printf 'event: done\ndata: {"seq":%s,"sent":%s}\n\n' "$((i + 1))" "$(cs)"
S
}

test_sse() {
  start_stack
  local net=${SELFHOST_PROJECT}_edge stub=$SELFHOST_PROJECT-sse-stub proxy=$SELFHOST_PROJECT-sse-proxy pport
  local line sent recv delay seq n=0 maxdelay=0 late=0 t0 t1 prev=0 done_seen=0 image=caddy:2.11-alpine
  pport=$(port)
  sse_stub_script >"$work/stub.sh"
  chmod 755 "$work/stub.sh"
  docker run -d --name "$stub" --network "$net" --network-alias sse-stub -v "$work/stub.sh:/stub.sh:ro" \
    --entrypoint sh "$image" -c 'while true; do nc -l -p 8080 -e /stub.sh; done' >/dev/null
  docker run -d --name "$proxy" --network "$net" -p "127.0.0.1:$pport:80" -e SITE_ADDRESS=:80 -e API_UPSTREAM=sse-stub:8080 \
    -v "$here/Caddyfile:/etc/caddy/Caddyfile:ro" "$image" >/dev/null
  wait_for 30 "stub proxy" docker exec "$proxy" wget -q -O /dev/null http://127.0.0.1:8081/healthz
  t0=$(date +%s)
  # --compressed sends Accept-Encoding: gzip, so this also proves `encode` leaves the stream alone.
  while IFS= read -r line; do
    recv=$(cs)
    case $line in
      event:*) done_seen=1 ;;
      data:*)
        sent=$(sed -n 's/.*"sent":\([0-9]*\).*/\1/p' <<<"$line")
        seq=$(sed -n 's/.*"seq":\([0-9]*\).*/\1/p' <<<"$line")
        delay=$(((recv - sent) * 10))
        printf 'sse: seq %s arrived %s ms after it was sent, %s s after the previous one\n' "$seq" "$delay" "$((prev ? (recv - prev) / 100 : 0))"
        if ((delay > maxdelay)); then maxdelay=$delay; fi
        if ((delay > 2000)); then late=$((late + 1)); fi
        prev=$recv
        if [[ $line != *'"seq":15,'* ]]; then n=$((n + 1)); fi
        ;;
    esac
  done < <(curl -sSN --compressed --max-time 240 "http://127.0.0.1:$pport/v1/stream" 2>"$work/curl.err")
  t1=$(date +%s)
  printf 'sse: %s data events in %s s, slowest delivery %s ms, curl stderr: %s\n' "$n" "$((t1 - t0))" "$maxdelay" "$(cat "$work/curl.err")"
  ((late == 0)) || die "$late event(s) arrived more than 2 s after they were sent: the proxy buffers"
  ((n == 14 && done_seen == 1)) || die "stream cut after $((t1 - t0)) s: $n of 14 events, final event seen=$done_seen"
  ((t1 - t0 >= 150)) || die "stream lasted only $((t1 - t0)) s; the test must hold it open for 150 s"
  say "sse passed"
}

# One upstream name, two replicas: one accepts the connection and drops it without answering (a replica
# dying mid-request), one answers. Without lb_try_duration Caddy gives up on the first dead answer (502);
# with it a GET is replayed until the live replica answers. No stack needed.
test_retry() {
  local net=$SELFHOST_PROJECT-retry proxy=$SELFHOST_PROJECT-retry-proxy pport i code bad=0 n=40 image=caddy:2.11-alpine
  pport=$(port)
  printf '#!/bin/sh\nprintf "HTTP/1.1 200 OK\\r\\ncontent-length: 2\\r\\nconnection: close\\r\\n\\r\\nok"\n' >"$work/good.sh"
  chmod 755 "$work/good.sh"
  docker network create "$net" >/dev/null
  docker run -d --name "$SELFHOST_PROJECT-retry-good" --network "$net" --network-alias flaky -v "$work/good.sh:/good.sh:ro" \
    --entrypoint sh "$image" -c 'while true; do nc -l -p 8080 -e /good.sh; done' >/dev/null
  docker run -d --name "$SELFHOST_PROJECT-retry-bad" --network "$net" --network-alias flaky \
    --entrypoint sh "$image" -c 'while true; do nc -l -p 8080 -e /bin/true; done' >/dev/null
  docker run -d --name "$proxy" --network "$net" -p "127.0.0.1:$pport:80" -e SITE_ADDRESS=:80 -e API_UPSTREAM=flaky:8080 \
    -v "$here/Caddyfile:/etc/caddy/Caddyfile:ro" "$image" >/dev/null
  wait_for 30 "retry proxy" docker exec "$proxy" wget -q -O /dev/null http://127.0.0.1:8081/healthz
  for ((i = 0; i < n; i++)); do
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 15 "http://127.0.0.1:$pport/v1/x" || true)
    [[ $code == 200 ]] || bad=$((bad + 1))
  done
  printf 'retry: %s of %s GETs failed with one replica dropping connections\n' "$bad" "$n"
  ((bad == 0)) || die "$bad GET(s) hit the dead replica and were not retried: the /v1 upstream needs lb_try_duration"
  say "retry passed"
}

case ${1:-} in
  retry) test_retry ;;
  sse) test_sse ;;
  rolling) test_rolling ;;
  lifecycle) test_lifecycle ;;
  e2e) test_e2e ;;
  ca) test_ca ;;
  pitr) test_pitr ;;
  redis) test_redis ;;
  dr) test_dr ;;
  *) sed -n '2,10p' "$0" >&2; exit 2 ;;
esac
