#!/usr/bin/env bash
# Fails when two Goose migrations under services/api/migrations share a version
# prefix: Goose rejects duplicates, so migrate and deploys would fail after both merge.
# Usage: migration-versions.sh [dir]
set -euo pipefail

dir=${1:-$(git rev-parse --show-toplevel)/services/api/migrations}

dups=$(find "$dir" -maxdepth 1 -name '*.sql' -printf '%f\n' | sed 's/_.*//' | sort | uniq -d)
if [[ -n $dups ]]; then
  while read -r version; do
    echo "duplicate migration version $version:" >&2
    find "$dir" -maxdepth 1 -name "${version}_*.sql" -printf '  %f\n' >&2
  done <<<"$dups"
  echo "Rename the later migration to a newer unused version." >&2
  exit 1
fi
