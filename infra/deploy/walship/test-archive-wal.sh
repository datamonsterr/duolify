#!/usr/bin/env bash
# Unit tests for archive-wal.sh, the Postgres archive_command (Postgres docs: return zero only on
# success, never overwrite an existing archive file, treat an identical one as success).
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/spool" "$work/pg_wal"
failures=0
arch() { WAL_SPOOL=$work/spool sh "$here/archive-wal.sh" "$@"; }
fail() { printf 'FAIL %s\n' "$1"; failures=$((failures + 1)); }
pass() { printf 'ok   %s\n' "$1"; }

printf 'segment-one' >"$work/pg_wal/000000010000000000000001"
arch "$work/pg_wal/000000010000000000000001" 000000010000000000000001 || fail "first archive of a segment"
[[ $(cat "$work/spool/000000010000000000000001") == segment-one ]] && pass "segment copied to the spool" || fail "segment content in the spool"
ls "$work/spool" | grep -q '\.tmp$' && fail "temp file left behind" || pass "no .tmp left behind"

arch "$work/pg_wal/000000010000000000000001" 000000010000000000000001 && pass "identical re-archive reports success" || fail "identical re-archive must succeed"

printf 'segment-DIFFERENT' >"$work/pg_wal/other"
if arch "$work/pg_wal/other" 000000010000000000000001; then fail "different content under an existing name must fail"; else pass "different content under an existing name is refused"; fi
[[ $(cat "$work/spool/000000010000000000000001") == segment-one ]] && pass "existing spool file untouched" || fail "existing spool file was overwritten"

if arch "$work/pg_wal/missing" 000000010000000000000002; then fail "missing source must fail"; else pass "missing source fails"; fi
[[ ! -e $work/spool/000000010000000000000002 ]] && pass "no partial file for a failed archive" || fail "partial file left for a failed archive"

if WAL_SPOOL=$work/nonexistent sh "$here/archive-wal.sh" "$work/pg_wal/000000010000000000000001" x; then fail "missing spool dir must fail"; else pass "missing spool directory fails"; fi

((failures == 0)) || { printf '%s failure(s)\n' "$failures" >&2; exit 1; }
