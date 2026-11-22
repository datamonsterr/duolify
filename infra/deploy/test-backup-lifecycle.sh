#!/usr/bin/env bash
# Unit tests for the generated lifecycle policy (no network): WAL must outlive the base backups
# it belongs to, the base must cover the PITR window, and storage must actually be freed.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
failures=0
t() { # NAME JQ-EXPR (must print true) [ENV=VAL]
  local name=$1 expr=$2 out
  shift 2
  out=$(env "$@" bash "$here/backup-lifecycle.sh" policy | jq -r "$expr") || out=error
  if [[ $out == true ]]; then printf 'ok   %s\n' "$name"; else printf 'FAIL %s (%s)\n' "$name" "$out"; failures=$((failures + 1)); fi
}
rule() { printf '.Rules[] | select(.Filter.Prefix=="%s") | %s' "$1" "$2"; }

t "covers exactly base/ and wal/" '[.Rules[].Filter.Prefix] | sort == ["base/","wal/"]' X=1
t "rules are enabled" '[.Rules[].Status] | all(. == "Enabled")' X=1
t "base/ outlives the 7-day PITR window by a night plus slack" "$(rule base/ '.Expiration.Days >= 9')" X=1
t "wal/ expires strictly later than base/" "([.Rules[] | select(.Filter.Prefix==\"wal/\") | .Expiration.Days][0]) > ([.Rules[] | select(.Filter.Prefix==\"base/\") | .Expiration.Days][0])" X=1
t "wal/ still later than base/ with a 14-day window" "([.Rules[] | select(.Filter.Prefix==\"wal/\") | .Expiration.Days][0]) > ([.Rules[] | select(.Filter.Prefix==\"base/\") | .Expiration.Days][0])" BACKUP_RETENTION_DAYS=14
t "14-day window moves base/ to 16 days" "$(rule base/ '.Expiration.Days == 16')" BACKUP_RETENTION_DAYS=14
t "noncurrent versions expire on both prefixes (versioned bucket)" '[.Rules[].NoncurrentVersionExpiration.NoncurrentDays] | all(. >= 1)' X=1
t "R2 (no versioning): no noncurrent rule, expiry kept" '([.Rules[] | has("NoncurrentVersionExpiration")] | any | not) and ([.Rules[].Expiration.Days] | all(. >= 9))' BACKUP_S3_VERSIONING=none
t "incomplete multipart uploads are aborted" '[.Rules[].AbortIncompleteMultipartUpload.DaysAfterInitiation] | all(. >= 1)' X=1
((failures == 0)) || { printf '%s failure(s)\n' "$failures" >&2; exit 1; }
