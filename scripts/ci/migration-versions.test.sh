#!/usr/bin/env bash
# Test for migration-versions.sh: unique versions pass, a duplicate fails.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
touch "$tmp/20260101000000_a.sql" "$tmp/20260101000100_b.sql"
bash "$here/migration-versions.sh" "$tmp"
touch "$tmp/20260101000000_c.sql"
if bash "$here/migration-versions.sh" "$tmp" 2>/dev/null; then
  echo "expected duplicate versions to fail" >&2
  exit 1
fi
echo ok
