#!/usr/bin/env bash
# Creates and versions S3_BUCKET and, when set, BACKUP_BUCKET (backups, WAL archive and DR copy
# must keep object versions).
set -euo pipefail

for bucket in "$S3_BUCKET" ${BACKUP_BUCKET:+"$BACKUP_BUCKET"}; do
  if ! aws s3api head-bucket --bucket "$bucket" >/dev/null 2>&1; then
    aws s3api create-bucket --bucket "$bucket"
  fi
  aws s3api put-bucket-versioning --bucket "$bucket" \
    --versioning-configuration Status=Enabled
done
