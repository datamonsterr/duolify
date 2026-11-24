#!/usr/bin/env bash
# Uploads the landing page media (landing.mp4, landing.webm, landing-poster.webp) to the public
# asset origin: <S3_BUCKET>/landing/<version>/. Served at $LESSON_ASSET_ORIGIN/landing/<version>/;
# set VITE_LANDING_MEDIA_BASE to that URL when building the web client. The object key carries the
# version, so the files are immutable: change the media, bump the version.
#
#   upload-landing-media.sh <dir> [version]     (default version: v1)
#
# An outward action: run it by hand against the target environment, never from CI.
set -euo pipefail
dir=${1:?usage: upload-landing-media.sh <dir> [version]}
version=${2:-v1}
: "${S3_BUCKET:?}" "${S3_ENDPOINT:?}" "${S3_ACCESS_KEY_ID:?}" "${S3_SECRET_ACCESS_KEY:?}"
export AWS_ACCESS_KEY_ID=$S3_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY=$S3_SECRET_ACCESS_KEY
export AWS_DEFAULT_REGION=${S3_REGION:-us-east-1}
declare -A types=([landing.mp4]=video/mp4 [landing.webm]=video/webm [landing-poster.webp]=image/webp)
for name in "${!types[@]}"; do
  [[ -f $dir/$name ]] || { echo "missing $dir/$name" >&2; exit 1; }
  aws s3 cp "$dir/$name" "s3://$S3_BUCKET/landing/$version/$name" --endpoint-url "$S3_ENDPOINT" \
    --content-type "${types[$name]}" --cache-control "public, max-age=31536000, immutable"
done
echo "uploaded; VITE_LANDING_MEDIA_BASE=${LESSON_ASSET_ORIGIN:-<asset origin>}/landing/$version"
