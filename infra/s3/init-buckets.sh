#!/usr/bin/env bash
set -euo pipefail

for bucket in "$S3_BUCKET" duolify-backups; do
  if ! aws s3api head-bucket --bucket "$bucket" >/dev/null 2>&1; then
    aws s3api create-bucket --bucket "$bucket"
  fi
done
aws s3api put-bucket-versioning --bucket "$S3_BUCKET" \
  --versioning-configuration Status=Enabled
