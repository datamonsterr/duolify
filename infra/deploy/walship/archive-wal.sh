#!/bin/sh
# Postgres archive_command: archive-wal.sh %p %f. Copies a WAL file into the spool that walship ships.
# Per the Postgres docs (continuous archiving): never overwrite an existing archive file; exit 0 only
# when the file is durably in the spool, including when an identical copy is already there
# (re-archive after a crash); exit non-zero when a different file holds that name.
set -eu
src=$1 name=$2
spool=${WAL_SPOOL:-/wal-spool}
dst=$spool/$name
if [ -e "$dst" ]; then
  cmp -s "$src" "$dst" || { echo "archive-wal: $dst exists with different content" >&2; exit 1; }
  sync "$dst" "$spool" # identical: make sure it is persisted before reporting success
  exit 0
fi
trap 'rm -f "$dst.tmp"' EXIT
cp "$src" "$dst.tmp"
sync "$dst.tmp"
mv "$dst.tmp" "$dst"
sync "$spool" # directory entry
