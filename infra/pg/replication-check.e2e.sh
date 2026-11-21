#!/usr/bin/env bash
# Integration test for replication-check.sh: two throwaway Postgres 18 containers joined by a
# publication and subscription. Covers the report, the PK-less failure and --compare mismatches.
set -euo pipefail
cd "$(dirname "$0")/../.."

id="duolify-pgrepl-$$"
net="$id"
pub="$id-pub"
sub="$id-sub"
cleanup() {
  docker rm -fv "$pub" "$sub" >/dev/null 2>&1 || true
  docker network rm "$net" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker network create "$net" >/dev/null
for node in "$pub" "$sub"; do
  docker run -d --name "$node" --network "$net" -e POSTGRES_PASSWORD=test postgres:18 \
    -c wal_level=logical >/dev/null
done
for node in "$pub" "$sub"; do
  for attempt in {1..60}; do
    docker exec "$node" pg_isready -U postgres >/dev/null 2>&1 && break
    [[ $attempt == 60 ]] && { echo "FAIL: $node not ready" >&2; exit 1; }
    sleep 1
  done
done

sql() { docker exec -i "$1" psql -U postgres -X -q -v ON_ERROR_STOP=1 -c "$2" >/dev/null; }
scalar() { docker exec -i "$1" psql -U postgres -X -A -t -c "$2"; }

schema='CREATE TABLE a(id int PRIMARY KEY, v text); CREATE TABLE b(id serial PRIMARY KEY, v text);'
sql "$pub" "$schema"
sql "$sub" "$schema"
sql "$pub" "CREATE PUBLICATION p FOR TABLE a, b"
sql "$sub" "CREATE SUBSCRIPTION s CONNECTION 'host=$pub user=postgres password=test dbname=postgres' PUBLICATION p"
sql "$pub" "INSERT INTO a SELECT g, 'row' || g FROM generate_series(1, 50) g; INSERT INTO b(v) SELECT 'b' || g FROM generate_series(1, 20) g; SELECT lo_create(0)"
for attempt in {1..60}; do
  [[ $(scalar "$sub" "SELECT (SELECT count(*) FROM a) + (SELECT count(*) FROM b)") == 70 ]] && break
  [[ $attempt == 60 ]] && { echo "FAIL: subscription did not sync" >&2; exit 1; }
  sleep 1
done

export PUBLISHER_URL="postgres://postgres:test@$pub:5432/postgres"
export SUBSCRIBER_URL="postgres://postgres:test@$sub:5432/postgres"
export PSQL_DOCKER_NETWORK="$net"
check="bash infra/pg/replication-check.sh"

fail() { echo "FAIL: $1" >&2; echo "$out" >&2; exit 1; }
expect_ok() { out=$($check "${@:2}" 2>&1) || fail "$1 (expected exit 0)"; }
expect_fail() { if out=$($check "${@:2}" 2>&1); then fail "$1 (expected non-zero exit)"; fi; }
expect_out() { grep -q -- "$1" <<<"$out" || fail "output lacks '$1'"; }

expect_ok "healthy replication"
expect_out "lag_bytes="
expect_out "lag_seconds="
expect_out "large_objects=1"
expect_out "sequence public.b_id_seq"
expect_out "tables_without_pk=0"

sql "$pub" "CREATE TABLE nopk(x int)"
expect_fail "planted PK-less table"
expect_out "nopk"
sql "$pub" "DROP TABLE nopk"

expect_ok "compare in sync" --compare
expect_out "compare ok"

sql "$sub" "UPDATE a SET v = 'tampered' WHERE id = 7"
expect_fail "checksum mismatch" --compare
expect_out "checksum mismatch: public.a"
sql "$sub" "UPDATE a SET v = 'row7' WHERE id = 7"
expect_ok "compare restored" --compare

sql "$sub" "DELETE FROM b WHERE id = 3"
expect_fail "row-count mismatch" --compare
expect_out "row count mismatch: public.b"

echo "OK: replication-check integration"
