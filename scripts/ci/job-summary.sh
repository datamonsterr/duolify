#!/usr/bin/env bash
# JOB_STATUS is supplied by the workflow; JOB_START by the setup action.
# Writes the job summary and repeats it in the log, so `gh run view --log-failed`
# shows failing tests and file:line too. Annotations come from junit-summary.py.
set -euo pipefail
cd "${MISE_PROJECT_ROOT:-.}"
now=$(date +%s)
elapsed=$((now - ${JOB_START:-$now}))
summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}

report=$(python3 -I scripts/ci/junit-summary.py || true)
body=$(
  printf '### %s\n\n' "${GITHUB_JOB:-local}"
  printf -- '- Status: **%s**\n' "${JOB_STATUS:-unknown}"
  printf -- '- Duration after checkout: %ss\n' "$elapsed"
  printf '%s\n' "$report" | grep -v '^::' || true
)
printf '%s\n' "$body" >> "$summary"
if [[ $summary != /dev/stdout ]]; then
  printf '%s\n' "$body"
fi
printf '%s\n' "$report" | grep '^::' || true
