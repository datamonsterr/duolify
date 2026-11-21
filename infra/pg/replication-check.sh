#!/usr/bin/env bash
# Logical-replication check between a publisher and a subscriber Postgres.
#   PUBLISHER_URL, SUBSCRIBER_URL   libpq URLs (required)
#   PSQL_DOCKER_NETWORK             docker network for the psql client (default: host)
#   COMPARE_SAMPLE_ROWS             rows per table in the checksum sample (default: 1000)
# Default mode reports lag (bytes, seconds), tables without a primary key (replica identity shown),
# large objects (never replicated) and sequences. Exits 1 on a PK-less table or no active
# replication. --compare instead checks row counts and a PK-ordered checksum sample per table.
set -euo pipefail

compare=false
[[ ${1:-} == --compare ]] && compare=true
: "${PUBLISHER_URL:?set PUBLISHER_URL}" "${SUBSCRIBER_URL:?set SUBSCRIBER_URL}"
sample=${COMPARE_SAMPLE_ROWS:-1000}

# psql runs in a postgres:18 container so the host needs no client.
q() { # url sql
  docker run --rm --network "${PSQL_DOCKER_NETWORK:-host}" postgres:18 \
    psql "$1" -X -A -t -F '|' -v ON_ERROR_STOP=1 -c "$2"
}

tables_sql="SELECT n.nspname, c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r','p') AND n.nspname NOT IN ('pg_catalog','information_schema','pg_toast')
  AND NOT c.relispartition ORDER BY 1, 2"

status=0

if $compare; then
  while IFS='|' read -r schema table; do
    [[ -z $table ]] && continue
    rel="\"$schema\".\"$table\""
    order=$(q "$PUBLISHER_URL" "SELECT coalesce(string_agg(quote_ident(a.attname), ',' ORDER BY k.ord), '1')
      FROM pg_index i CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY k(attnum, ord)
      JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
      WHERE i.indrelid = '$rel'::regclass AND i.indisprimary")
    count_sql="SELECT count(*) FROM $rel"
    sum_sql="SELECT coalesce(md5(string_agg(x, '|' ORDER BY x)), '') FROM (SELECT t::text x FROM $rel t ORDER BY $order LIMIT $sample) s"
    pc=$(q "$PUBLISHER_URL" "$count_sql"); sc=$(q "$SUBSCRIBER_URL" "$count_sql")
    if [[ $pc != "$sc" ]]; then
      echo "row count mismatch: $schema.$table publisher=$pc subscriber=$sc"; status=1; continue
    fi
    if [[ $(q "$PUBLISHER_URL" "$sum_sql") != "$(q "$SUBSCRIBER_URL" "$sum_sql")" ]]; then
      echo "checksum mismatch: $schema.$table (first $sample rows by $order)"; status=1; continue
    fi
    echo "ok: $schema.$table rows=$pc"
  done < <(q "$PUBLISHER_URL" "$tables_sql")
  ((status == 0)) && echo "compare ok"
  exit "$status"
fi

# Lag from the publisher's logical walsender: bytes behind current WAL, replay lag in seconds.
lag=$(q "$PUBLISHER_URL" "SELECT application_name, pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn)::bigint,
  coalesce(extract(epoch FROM replay_lag), 0)::numeric(12,3) FROM pg_stat_replication ORDER BY 1")
if [[ -z $lag ]]; then
  echo "replication: no active subscriber walsender on the publisher"; status=1
else
  while IFS='|' read -r app bytes seconds; do
    echo "subscription=$app lag_bytes=$bytes lag_seconds=$seconds"
  done <<<"$lag"
fi

nopk=$(q "$PUBLISHER_URL" "SELECT n.nspname || '.' || c.relname || ' replica_identity=' ||
  CASE c.relreplident WHEN 'd' THEN 'default' WHEN 'n' THEN 'nothing' WHEN 'f' THEN 'full' ELSE 'index' END
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r','p') AND n.nspname NOT IN ('pg_catalog','information_schema','pg_toast')
  AND NOT c.relispartition
  AND NOT EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = c.oid AND i.indisprimary) ORDER BY 1")
if [[ -n $nopk ]]; then
  echo "tables_without_pk=$(wc -l <<<"$nopk")"
  while read -r line; do echo "  no primary key: $line"; done <<<"$nopk"
  status=1
else
  echo "tables_without_pk=0"
fi

echo "large_objects=$(q "$PUBLISHER_URL" 'SELECT count(*) FROM pg_largeobject_metadata') (not replicated; migrate separately)"

echo "sequences (publisher last_value | subscriber last_value):"
while IFS='|' read -r name value; do
  [[ -z $name ]] && continue
  other=$(q "$SUBSCRIBER_URL" "SELECT coalesce(last_value::text, 'null') FROM pg_sequences WHERE schemaname || '.' || sequencename = '$name'")
  echo "sequence $name ${value:-null} | ${other:-missing}"
done < <(q "$PUBLISHER_URL" "SELECT schemaname || '.' || sequencename, coalesce(last_value::text, 'null') FROM pg_sequences ORDER BY 1")

exit "$status"
