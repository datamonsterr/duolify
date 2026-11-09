#!/usr/bin/env bash
set -euo pipefail

if ! aws s3api head-bucket --bucket "$S3_BUCKET" >/dev/null 2>&1; then
  aws s3api create-bucket --bucket "$S3_BUCKET"
fi
aws s3api put-bucket-versioning --bucket "$S3_BUCKET" \
  --versioning-configuration Status=Enabled
