#!/usr/bin/env bash
# JOB_STATUS is supplied by the workflow; JOB_START by the setup action.
set -euo pipefail
now=$(date +%s)
elapsed=$((now - ${JOB_START:-$now}))
{
  printf '### %s\n\n' "${GITHUB_JOB:-local}"
  printf -- '- Status: **%s**\n' "${JOB_STATUS:-unknown}"
  printf -- '- Duration after checkout: %ss\n' "$elapsed"
} >> "${GITHUB_STEP_SUMMARY:-/dev/stdout}"
