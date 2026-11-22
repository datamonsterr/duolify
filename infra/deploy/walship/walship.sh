#!/usr/bin/env bash
# Ships Postgres backups off the primary site. Everything is gzip'd and age-encrypted
# (public recipient only) before it leaves the host, into the backup bucket
# (BACKUP_S3_*, which in production is a Cloudflare R2 bucket, not the Helsinki VM):
#
#   ship     move archived WAL segments from /wal-spool to wal/<segment>.gz.age
#   base     pg_basebackup to base/<UTC timestamp>/base.tar.gz.age (the PITR starting point)
#   dr-copy  copy new or changed objects of the app bucket (S3_*) to dr/lesson-assets/ in the
#            backup bucket; never deletes (a versioned bucket also keeps every version; R2 has no versioning)
#   restore  PITR drill in a throwaway Postgres: newest base before the target time plus archived WAL,
#            recovered to TARGET (e.g. '2026-11-21 10:00:00+00'); needs the age identity at /identity
#   metrics  write Prometheus textfile metrics to /metrics/walship.prom
#   daily    base, then dr-copy (as WALSHIP_JOBS allows); non-zero if either failed
#   loop     ship every 10s, metrics every minute, base + dr-copy daily at 03:00 UTC
#            (WALSHIP_JOBS selects which of ship,base,dr-copy run)
#
# Env: PGHOST PGUSER PGPASSWORD, BACKUP_S3_{ENDPOINT,REGION,BUCKET,ACCESS_KEY_ID,SECRET_ACCESS_KEY},
#      S3_{ENDPOINT,REGION,BUCKET,ACCESS_KEY_ID,SECRET_ACCESS_KEY}, BACKUP_AGE_RECIPIENT,
#      S3_ADDRESSING_STYLE (path|virtual, default path).
set -euo pipefail

spool=${WALSHIP_SPOOL:-/wal-spool}
state=${WALSHIP_STATE:-/metrics}
export AWS_REQUEST_CHECKSUM_CALCULATION=when_required AWS_RESPONSE_CHECKSUM_VALIDATION=when_required AWS_PAGER=""
export AWS_CONFIG_FILE=${AWS_CONFIG_FILE:-/tmp/aws-config}
printf '[default]\ns3 =\n    addressing_style = %s\n' "${S3_ADDRESSING_STYLE:-path}" >"$AWS_CONFIG_FILE"

dest() {
  AWS_ENDPOINT_URL=$BACKUP_S3_ENDPOINT AWS_DEFAULT_REGION=$BACKUP_S3_REGION \
    AWS_ACCESS_KEY_ID=$BACKUP_S3_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY=$BACKUP_S3_SECRET_ACCESS_KEY aws "$@"
}
src() {
  AWS_ENDPOINT_URL=$S3_ENDPOINT AWS_DEFAULT_REGION=$S3_REGION \
    AWS_ACCESS_KEY_ID=$S3_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY=$S3_SECRET_ACCESS_KEY aws "$@"
}
log() { printf '%s walship: %s\n' "$(date -u +%FT%TZ)" "$*" >&2; }
stamp() { date -u +%s >"$state/$1.ts"; } # last-success marker read by metrics

# Segments are immutable: a name that already exists in the bucket must hold the same bytes.
# The plaintext sha256 travels as object metadata (age output is not deterministic).
cmd_ship() {
  local f name sha prior key
  for f in "$spool"/*; do
    [[ -f $f && $f != *.tmp ]] || continue
    name=$(basename "$f")
    key=wal/$name.gz.age
    sha=$(sha256sum "$f" | cut -d' ' -f1) || return 1
    prior=$(dest s3api head-object --bucket "$BACKUP_S3_BUCKET" --key "$key" --query 'Metadata."src-sha256"' --output text 2>/dev/null || true)
    if [[ -n $prior && $prior != None ]]; then
      if [[ $prior == "$sha" ]]; then rm -f "$f"; continue; fi # re-archived after a crash: already shipped
      log "REFUSING $name: $key exists with different content; the segment stays in the spool"
      return 1
    fi
    if gzip -c "$f" | age -r "$BACKUP_AGE_RECIPIENT" | dest s3 cp - "s3://$BACKUP_S3_BUCKET/$key" --metadata "src-sha256=$sha" --only-show-errors; then
      rm -f "$f"
    else
      log "could not ship $name; it stays in the spool"
      return 1
    fi
  done
}

# Every step is checked explicitly: callers use `cmd || log`, which disables `set -e` in here.
# The success stamp (read by the age alerts) is written only after the whole pipeline succeeded.
cmd_base() {
  local at
  at=$(date -u +%Y%m%dT%H%M%SZ) || return 1
  if ! { pg_basebackup --dbname "host=${PGHOST:-postgres} user=${PGUSER:-duolify}" -D - -Ft -X none --checkpoint=fast --no-sync \
    | gzip | age -r "$BACKUP_AGE_RECIPIENT" \
    | dest s3 cp - "s3://$BACKUP_S3_BUCKET/base/$at/base.tar.gz.age" --only-show-errors; }; then
    log "base backup base/$at FAILED; not stamping"
    # A truncated upload must never be picked as the newest base by a restore.
    dest s3 rm "s3://$BACKUP_S3_BUCKET/base/$at/base.tar.gz.age" --only-show-errors >/dev/null 2>&1 || true
    return 1
  fi
  stamp base || return 1
  log "base backup base/$at uploaded"
}

cmd_dr_copy() {
  local key etag prior prefix=dr/lesson-assets/ copied=0 failed=0 listing
  listing=$(src s3api list-objects-v2 --bucket "$S3_BUCKET" --query 'Contents[].[Key,ETag]' --output text) || { log "DR copy: cannot list $S3_BUCKET"; return 1; }
  while IFS=$'\t' read -r key etag; do
    [[ -n $key && $key != None ]] || continue
    prior=$(dest s3api head-object --bucket "$BACKUP_S3_BUCKET" --key "$prefix$key" --query 'Metadata."src-etag"' --output text 2>/dev/null || true)
    [[ $prior != "$etag" ]] || continue
    if { src s3 cp "s3://$S3_BUCKET/$key" - --only-show-errors \
      | dest s3 cp - "s3://$BACKUP_S3_BUCKET/$prefix$key" --metadata "src-etag=$etag" --only-show-errors; }; then
      copied=$((copied + 1))
    else
      log "DR copy: could not copy $key"
      failed=$((failed + 1))
    fi
  done <<<"$listing"
  if ((failed > 0)); then log "DR copy FAILED for $failed object(s); not stamping"; return 1; fi
  stamp dr || return 1
  log "DR copy done: $copied object(s) copied"
}

cmd_restore() { # TARGET_TIME: recover a throwaway Postgres from base/ + wal/ and print what it holds
  local target=${1:?target time} limit base key name labels
  limit=$(date -u -d "$target" +%Y%m%dT%H%M%SZ)
  base=$(dest s3api list-objects-v2 --bucket "$BACKUP_S3_BUCKET" --prefix base/ --query 'Contents[].Key' --output text \
    | tr '\t' '\n' | grep 'base.tar.gz.age$' | awk -F/ -v limit="$limit" '$2 <= limit' | sort | tail -n 1)
  [[ -n $base ]] || { log "no base backup at or before $target"; return 1; }
  mkdir -p /restore/data /restore/wal
  dest s3 cp "s3://$BACKUP_S3_BUCKET/$base" - --only-show-errors | age -d -i /identity | gunzip | tar -x -C /restore/data
  for key in $(dest s3api list-objects-v2 --bucket "$BACKUP_S3_BUCKET" --prefix wal/ --query 'Contents[].Key' --output text); do
    name=$(basename "$key" .gz.age)
    dest s3 cp "s3://$BACKUP_S3_BUCKET/$key" - --only-show-errors | age -d -i /identity | gunzip >"/restore/wal/$name"
  done
  touch /restore/data/recovery.signal
  chown -R postgres:postgres /restore
  chmod 700 /restore/data
  gosu postgres pg_ctl -D /restore/data -w -t 300 -l /restore/log start -o "-c listen_addresses='' -c unix_socket_directories=/tmp -c archive_mode=off \
    -c restore_command='cp /restore/wal/%f %p' -c recovery_target_time='$target' -c recovery_target_action=promote" >/dev/null
  until [[ $(psql -X -At -h /tmp -U "${PGUSER:-duolify}" -d duolify -c 'select pg_is_in_recovery()') == f ]]; do sleep 1; done
  labels=$(psql -X -At -h /tmp -U "${PGUSER:-duolify}" -d duolify -c "select coalesce(string_agg(label, ',' order by ctid), '') from pitr_marker" 2>/dev/null || true)
  psql -X -At -h /tmp -U "${PGUSER:-duolify}" -d duolify -c 'select count(*) from goose_db_version' >/dev/null
  printf 'pitr_base=%s\npitr_labels=%s\n' "$base" "$labels"
  gosu postgres pg_ctl -D /restore/data -m immediate stop >/dev/null
}

bucket_bytes() { # BUCKET -> total bytes of the current objects
  "$2" s3api list-objects-v2 --bucket "$1" --query 'sum(Contents[].Size)' --output text 2>/dev/null | sed 's/^None$/0/' || printf 0
}

cmd_metrics() {
  local now oldest files=0 age=0 f out=$state/walship.prom stats
  now=$(date -u +%s)
  oldest=$now
  for f in "$spool"/*; do
    [[ -f $f && $f != *.tmp ]] || continue
    files=$((files + 1))
    if [[ $(stat -c %Y "$f") -lt $oldest ]]; then oldest=$(stat -c %Y "$f"); fi
  done
  age=$((now - oldest))
  stats=$(psql -X -At -F' ' -c "select failed_count from pg_stat_archiver" 2>/dev/null || true)
  {
    printf 'duolify_wal_spool_files %s\nduolify_wal_spool_oldest_age_seconds %s\n' "$files" "$age"
    if [[ -n $stats ]]; then
      printf 'duolify_pg_archive_failed_total %s\n' "$(sed -n 1p <<<"$stats")"
    fi
    for f in base dr; do
      if [[ -f $state/$f.ts ]]; then printf 'duolify_%s_last_success_timestamp_seconds %s\n' "$f" "$(cat "$state/$f.ts")"; fi
    done
    for f in $(compgen -G "$state/bucket-*.bytes" || true); do
      printf 'duolify_bucket_bytes{bucket="%s"} %s\n' "$(basename "$f" .bytes | sed 's/^bucket-//')" "$(cat "$f")"
    done
    if [[ -f $state/base-remote.ts ]]; then printf 'duolify_base_newest_object_timestamp_seconds %s\n' "$(cat "$state/base-remote.ts")"; fi
    if [[ -f $state/dump.ts ]]; then printf 'duolify_dump_last_object_timestamp_seconds %s\n' "$(cat "$state/dump.ts")"; fi
  } >"$out.tmp"
  mv "$out.tmp" "$out"
}

cmd_slow_metrics() { # hourly: bucket usage and the newest nightly application dump
  local newest
  bucket_bytes "$BACKUP_S3_BUCKET" dest >"$state/bucket-$BACKUP_S3_BUCKET.bytes"
  bucket_bytes "$S3_BUCKET" src >"$state/bucket-$S3_BUCKET.bytes"
  # shellcheck disable=SC2016 # JMESPath backticks are literals, not shell expansion
  newest=$(dest s3api list-objects-v2 --bucket "$BACKUP_S3_BUCKET" \
    --query 'sort_by(Contents[?contains(Key, `/schema-`)], &LastModified)[-1].LastModified' --output text 2>/dev/null || true)
  if [[ -n $newest && $newest != None ]]; then date -u -d "$newest" +%s >"$state/dump.ts"; fi
  # Newest base backup actually in the bucket (not just the local success stamp): PITR needs one.
  # A definite "none" removes the file so the metric disappears and the absent() alert fires.
  # shellcheck disable=SC2016 # JMESPath
  if newest=$(dest s3api list-objects-v2 --bucket "$BACKUP_S3_BUCKET" --prefix base/ \
    --query "sort_by(Contents[?ends_with(Key, 'base.tar.gz.age')], &LastModified)[-1].LastModified" --output text 2>/dev/null); then
    if [[ -n $newest && $newest != None ]]; then date -u -d "$newest" +%s >"$state/base-remote.ts"; else rm -f "$state/base-remote.ts"; fi
  fi
}

has_job() { [[ ,${WALSHIP_JOBS:-ship,base,dr-copy}, == *",$1,"* ]]; }

cmd_daily() { # the nightly jobs; non-zero if any failed
  local rc=0
  if has_job base; then cmd_base || { log "base backup failed"; rc=1; }; fi
  if has_job dr-copy; then cmd_dr_copy || { log "DR copy failed"; rc=1; }; fi
  return $rc
}

cmd_loop() {
  local tick=0 last_day='' job=0 day
  mkdir -p "$state"
  until pg_isready -q -h "${PGHOST:-postgres}"; do sleep 2; done
  while true; do
    has_job ship && { cmd_ship || true; }
    if ((tick % 6 == 0)); then cmd_metrics || true; fi
    if ((tick % 360 == 0)); then cmd_slow_metrics || true; fi
    day=$(date -u +%F)
    # Daily jobs run in the background: pg_basebackup waits for its last WAL segment to be shipped.
    if (( 10#$(date -u +%H%M) >= 300 )) && [[ $last_day != "$day" ]] || { has_job base && [[ ! -f $state/base.ts && -z $last_day ]]; }; then
      last_day=$day
      if ! kill -0 "$job" 2>/dev/null; then
        cmd_daily &
        job=$!
      fi
    fi
    tick=$((tick + 1))
    sleep 10
  done
}

case ${1:-loop} in
  ship) cmd_ship ;;
  base) cmd_base ;;
  dr-copy) cmd_dr_copy ;;
  daily) cmd_daily ;;
  restore) shift; cmd_restore "$@" ;;
  metrics) cmd_metrics ;;
  loop) cmd_loop ;;
  *) sed -n '2,12p' "$0" >&2; exit 2 ;;
esac
