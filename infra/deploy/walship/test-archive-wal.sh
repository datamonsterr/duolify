#!/usr/bin/env bash
# shellcheck disable=SC2016 # the bash -c snippets are meant to expand in the child
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
check() { local name=$1; shift; if "$@"; then pass "$name"; else fail "$name"; fi; }
content_is() { [[ $(cat "$1") == "$2" ]]; }

printf 'segment-one' >"$work/pg_wal/000000010000000000000001"
seg=000000010000000000000001
check "first archive of a segment" arch "$work/pg_wal/$seg" $seg
check "segment content in the spool" content_is "$work/spool/$seg" segment-one
check "no .tmp left behind" bash -c '! compgen -G "$1/*.tmp" >/dev/null' _ "$work/spool"
check "identical re-archive reports success" arch "$work/pg_wal/$seg" $seg

printf 'segment-DIFFERENT' >"$work/pg_wal/other"
check "different content under an existing name is refused" bash -c '! "$@"' _ arch "$work/pg_wal/other" $seg
check "existing spool file untouched" content_is "$work/spool/$seg" segment-one
check "missing source fails" bash -c '! "$@"' _ arch "$work/pg_wal/missing" 000000010000000000000002
check "no partial file for a failed archive" bash -c '! compgen -G "$1/*0002*" >/dev/null' _ "$work/spool"
check "missing spool directory fails" bash -c '! WAL_SPOOL=$1 sh "$2" "$3" x' _ "$work/nonexistent" "$here/archive-wal.sh" "$work/pg_wal/$seg"

((failures == 0)) || { printf '%s failure(s)\n' "$failures" >&2; exit 1; }
