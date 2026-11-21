#!/usr/bin/env bash
# Retention of the backup bucket's wal/ and base/ prefixes as an S3 lifecycle configuration.
#
#   backup-lifecycle.sh policy              print the lifecycle JSON (no network)
#   backup-lifecycle.sh apply               put it on the bucket (admin key)
#   backup-lifecycle.sh verify [--require-base]
#                                           preflight: versioning on, lifecycle matches the policy,
#                                           and (with --require-base) a base newer than the PITR window exists
#
# Env: BACKUP_S3_ENDPOINT BACKUP_S3_REGION BACKUP_S3_BUCKET (as in .env), and an ADMIN key that is
# NOT on the VM: BACKUP_ADMIN_S3_ACCESS_KEY_ID BACKUP_ADMIN_S3_SECRET_ACCESS_KEY.
# BACKUP_RETENTION_DAYS (default 7) is the PITR window.
#
# Why these numbers. To recover to any time T in the last R days you need the newest base taken at or
# before T, i.e. the base from just before now-R (bases are nightly), plus every WAL segment after it.
#   base/  expire after R+2 days: R, plus one nightly interval, plus one missed night of slack.
#   wal/   expire after R+3 days: strictly LATER than base/, so every base that still exists
#          has all the WAL written after it. Never the other way round.
# The bucket is versioned, so Expiration only adds a delete marker; NoncurrentVersionExpiration (1 day)
# is what actually frees the storage, and expired delete markers are cleaned up. Because objects are
# written once, the effective hard retention is the numbers above plus that 1 day.
# The lifecycle cannot know which base is newest: a stopped base job is caught by the
# DuolifyBackupStale / DuolifyNoRecentBaseInBucket alerts (36 h) long before base/ would drain (R+2 days).
set -euo pipefail

retention=${BACKUP_RETENTION_DAYS:-7}
base_days=$((retention + 2))
wal_days=$((retention + 3))

policy() {
  cat <<JSON
{"Rules":[
{"ID":"duolify-base","Status":"Enabled","Filter":{"Prefix":"base/"},"Expiration":{"Days":$base_days},"NoncurrentVersionExpiration":{"NoncurrentDays":1},"AbortIncompleteMultipartUpload":{"DaysAfterInitiation":1}},
{"ID":"duolify-wal","Status":"Enabled","Filter":{"Prefix":"wal/"},"Expiration":{"Days":$wal_days},"NoncurrentVersionExpiration":{"NoncurrentDays":1},"AbortIncompleteMultipartUpload":{"DaysAfterInitiation":1}}
]}
JSON
}

admin() {
  : "${BACKUP_S3_ENDPOINT:?}" "${BACKUP_S3_REGION:?}" "${BACKUP_S3_BUCKET:?}" "${BACKUP_ADMIN_S3_ACCESS_KEY_ID:?admin key needed}" "${BACKUP_ADMIN_S3_SECRET_ACCESS_KEY:?admin key needed}"
  AWS_ENDPOINT_URL=$BACKUP_S3_ENDPOINT AWS_DEFAULT_REGION=$BACKUP_S3_REGION AWS_PAGER="" \
    AWS_REQUEST_CHECKSUM_CALCULATION=when_required AWS_RESPONSE_CHECKSUM_VALIDATION=when_required \
    AWS_ACCESS_KEY_ID=$BACKUP_ADMIN_S3_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY=$BACKUP_ADMIN_S3_SECRET_ACCESS_KEY aws "$@"
}

cmd_apply() {
  admin s3api put-bucket-versioning --bucket "$BACKUP_S3_BUCKET" --versioning-configuration Status=Enabled
  admin s3api put-bucket-lifecycle-configuration --bucket "$BACKUP_S3_BUCKET" --lifecycle-configuration "$(policy)"
  echo "backup-lifecycle: applied to $BACKUP_S3_BUCKET (base/ ${base_days}d, wal/ ${wal_days}d, noncurrent 1d)"
}

rule_value() { # PREFIX JMESPATH-after-rule -> value or empty
  local out
  out=$(admin s3api get-bucket-lifecycle-configuration --bucket "$BACKUP_S3_BUCKET" \
    --query "Rules[?Status=='Enabled' && (Filter.Prefix=='$1' || Prefix=='$1')] | [0].$2" --output text 2>/dev/null) || return 1
  [[ $out == None ]] && out=''
  printf '%s' "$out"
}

cmd_verify() {
  local problems=0 v bd wd nb nw newest age
  bad() { echo "backup-lifecycle: FAIL $*" >&2; problems=$((problems + 1)); }
  v=$(admin s3api get-bucket-versioning --bucket "$BACKUP_S3_BUCKET" --query Status --output text 2>/dev/null) || v=''
  [[ $v == Enabled ]] || bad "bucket $BACKUP_S3_BUCKET is not versioned (Status='$v')"
  bd=$(rule_value base/ Expiration.Days) || bd=''
  wd=$(rule_value wal/ Expiration.Days) || wd=''
  nb=$(rule_value base/ NoncurrentVersionExpiration.NoncurrentDays) || nb=''
  nw=$(rule_value wal/ NoncurrentVersionExpiration.NoncurrentDays) || nw=''
  [[ $bd =~ ^[0-9]+$ ]] || bad "no enabled lifecycle rule expiring base/ (storage grows without bound)"
  [[ $wd =~ ^[0-9]+$ ]] || bad "no enabled lifecycle rule expiring wal/ (storage grows without bound)"
  [[ $nb =~ ^[0-9]+$ ]] || bad "base/ rule has no NoncurrentVersionExpiration (old versions are never freed)"
  [[ $nw =~ ^[0-9]+$ ]] || bad "wal/ rule has no NoncurrentVersionExpiration (old versions are never freed)"
  if [[ $bd =~ ^[0-9]+$ && $bd -lt $base_days ]]; then bad "base/ expires after $bd days, less than the $base_days needed for a $retention-day PITR window"; fi
  if [[ $bd =~ ^[0-9]+$ && $wd =~ ^[0-9]+$ && $wd -le $bd ]]; then bad "wal/ ($wd d) must expire later than base/ ($bd d), or the oldest base loses its WAL"; fi
  if [[ ${1:-} == --require-base ]]; then
    # shellcheck disable=SC2016 # JMESPath
    newest=$(admin s3api list-objects-v2 --bucket "$BACKUP_S3_BUCKET" --prefix base/ \
      --query "sort_by(Contents[?ends_with(Key, 'base.tar.gz.age')], &LastModified)[-1].LastModified" --output text 2>/dev/null) || newest=''
    if [[ -z $newest || $newest == None ]]; then bad "no base backup in $BACKUP_S3_BUCKET; PITR is impossible"
    else
      age=$(( $(date -u +%s) - $(date -u -d "$newest" +%s) ))
      if ((age > retention * 86400)); then bad "newest base backup is $((age / 3600)) h old, older than the $retention-day window"; fi
    fi
  fi
  ((problems == 0)) || return 1
  echo "backup-lifecycle: ok (base/ ${bd}d, wal/ ${wd}d)"
}

case ${1:-} in
  policy) policy ;;
  apply) cmd_apply ;;
  verify) shift; cmd_verify "$@" ;;
  *) sed -n '2,10p' "$0" >&2; exit 2 ;;
esac
