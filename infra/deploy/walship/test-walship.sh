#!/usr/bin/env bash
# Unit tests for walship.sh with stubbed aws, pg_basebackup and age (no Docker, no network):
# a failed upload or copy must exit non-zero and must NOT refresh the success stamp the age
# alerts read; ship must never overwrite a different archived segment.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/spool" "$work/state"
failures=0

cat >"$work/bin/aws" <<'STUB'
#!/usr/bin/env bash
# Fake aws. Source endpoint http://src, backup endpoint http://dest.
args="$*"
echo "$AWS_ENDPOINT_URL $args" >>"$FAKE_LOG"
if [[ $AWS_ENDPOINT_URL == http://src ]]; then
  if [[ $args == *list-objects-v2* ]]; then printf 'a.txt\tetag-a\nb.txt\tetag-b\n'; exit 0; fi
  if [[ $args == s3\ cp* ]]; then
    if [[ -n ${FAKE_SRC_FAIL:-} && $args == *"$FAKE_SRC_FAIL"* ]]; then exit 1; fi
    echo payload; exit 0
  fi
  exit 0
fi
[[ -z ${FAKE_DEST_DOWN:-} ]] || { cat >/dev/null 2>&1 || true; exit 1; }
if [[ $args == *head-object* ]]; then
  [[ -n ${FAKE_HEAD_SHA:-} ]] || exit 254
  echo "$FAKE_HEAD_SHA"; exit 0
fi
if [[ $args == s3\ cp\ -* ]]; then cat >/dev/null; exit 0; fi
exit 0
STUB
cat >"$work/bin/pg_basebackup" <<'STUB'
#!/usr/bin/env bash
[[ -z ${FAKE_PGBB_FAIL:-} ]] || exit 1
echo base-tar
STUB
printf '#!/usr/bin/env bash\ncat\n' >"$work/bin/age"
chmod +x "$work/bin/"*

run() { # args to walship.sh; env of the test is inherited
  PATH="$work/bin:$PATH" WALSHIP_SPOOL=$work/spool WALSHIP_STATE=$work/state AWS_CONFIG_FILE=$work/aws-config FAKE_LOG=$work/log \
    BACKUP_S3_ENDPOINT=http://dest BACKUP_S3_REGION=r BACKUP_S3_BUCKET=bk BACKUP_S3_ACCESS_KEY_ID=k BACKUP_S3_SECRET_ACCESS_KEY=s \
    S3_ENDPOINT=http://src S3_REGION=r S3_BUCKET=app S3_ACCESS_KEY_ID=k S3_SECRET_ACCESS_KEY=s BACKUP_AGE_RECIPIENT=age1x PGHOST=h \
    bash "$here/walship.sh" "$@" >"$work/out" 2>&1
}

check() { # NAME EXPECTED-RC STAMP-FILE-OR-"" EXPECT-STAMP-CHANGED(yes|no) -- command...
  local name=$1 want=$2 stamp=$3 changed=$4 rc=0 before='' after=''
  shift 5
  [[ -z $stamp || ! -f $work/state/$stamp ]] || before=$(cat "$work/state/$stamp")
  "$@" || rc=$?
  [[ -z $stamp || ! -f $work/state/$stamp ]] || after=$(cat "$work/state/$stamp")
  if [[ $want == ok && $rc != 0 ]] || [[ $want == fail && $rc == 0 ]]; then
    printf 'FAIL %s: exit code %s (wanted %s)\n' "$name" "$rc" "$want"; cat "$work/out"; failures=$((failures + 1)); return
  fi
  if [[ $changed == yes && $before == "$after" ]] || [[ $changed == no && $before != "$after" ]]; then
    printf 'FAIL %s: stamp %s before=%s after=%s (changed? wanted %s)\n' "$name" "$stamp" "$before" "$after" "$changed"; failures=$((failures + 1)); return
  fi
  printf 'ok   %s\n' "$name"
}

echo 1 >"$work/state/base.ts"; echo 1 >"$work/state/dr.ts"
check "base: bucket unreachable exits non-zero, keeps old stamp" fail base.ts no -- env FAKE_DEST_DOWN=1 bash -c "$(declare -f run); work=$work here=$here; run base"
check "base: pg_basebackup failure exits non-zero, keeps old stamp" fail base.ts no -- env FAKE_PGBB_FAIL=1 bash -c "$(declare -f run); work=$work here=$here; run base"
check "base: success stamps" ok base.ts yes -- bash -c "$(declare -f run); work=$work here=$here; run base"
check "dr-copy: bucket unreachable exits non-zero, keeps old stamp" fail dr.ts no -- env FAKE_DEST_DOWN=1 bash -c "$(declare -f run); work=$work here=$here; run dr-copy"
check "dr-copy: one failed object exits non-zero, keeps old stamp" fail dr.ts no -- env FAKE_SRC_FAIL=a.txt bash -c "$(declare -f run); work=$work here=$here; run dr-copy"
check "dr-copy: success stamps" ok dr.ts yes -- bash -c "$(declare -f run); work=$work here=$here; run dr-copy"

# The nightly path (loop -> daily) runs base and dr-copy as `cmd || log`, where set -e is off.
check "daily: bucket unreachable exits non-zero, keeps the base stamp" fail base.ts no -- env FAKE_DEST_DOWN=1 bash -c "$(declare -f run); work=$work here=$here; run daily"
check "daily: bucket unreachable keeps the dr stamp" fail dr.ts no -- env FAKE_DEST_DOWN=1 bash -c "$(declare -f run); work=$work here=$here; run daily"
check "daily: pg_basebackup failure exits non-zero, base stamp kept" fail base.ts no -- env FAKE_PGBB_FAIL=1 bash -c "$(declare -f run); work=$work here=$here; run daily"
check "daily: one failed DR object exits non-zero, dr stamp kept" fail dr.ts no -- env FAKE_SRC_FAIL=a.txt bash -c "$(declare -f run); work=$work here=$here; run daily"

printf 'segment-bytes' >"$work/spool/000000010000000000000001"
sha=$(sha256sum "$work/spool/000000010000000000000001" | cut -d' ' -f1)
check "ship: segment already archived with different content is refused and stays spooled" fail "" no -- env FAKE_HEAD_SHA=deadbeef bash -c "$(declare -f run); work=$work here=$here; run ship"
[[ -f $work/spool/000000010000000000000001 ]] || { echo "FAIL ship: spool file lost after refusal"; failures=$((failures + 1)); }
check "ship: identical re-archived segment is accepted and dropped from the spool" ok "" no -- env FAKE_HEAD_SHA="$sha" bash -c "$(declare -f run); work=$work here=$here; run ship"
[[ ! -f $work/spool/000000010000000000000001 ]] || { echo "FAIL ship: identical segment not removed"; failures=$((failures + 1)); }
printf 'segment-bytes' >"$work/spool/000000010000000000000002"
check "ship: new segment uploaded and removed from the spool" ok "" no -- bash -c "$(declare -f run); work=$work here=$here; run ship"
[[ ! -f $work/spool/000000010000000000000002 ]] || { echo "FAIL ship: new segment not removed"; failures=$((failures + 1)); }

((failures == 0)) || { printf '%s failure(s)\n' "$failures" >&2; exit 1; }
