#!/usr/bin/env bash
# Usage: REQUIRED_SECRETS="A B" require-secrets.sh <what is skipped>
# Each name must be set in the environment (the workflow maps secrets to env).
# Writes enabled=true|false to $GITHUB_OUTPUT. Never fails: a missing secret
# produces a notice and a summary line, so forks and secret-less repos stay green.
set -euo pipefail
what=${1:-this check}
missing=()
for name in ${REQUIRED_SECRETS:?REQUIRED_SECRETS required}; do
  [[ -n ${!name:-} ]] || missing+=("$name")
done
out=${GITHUB_OUTPUT:-/dev/null}
if ((${#missing[@]} > 0)); then
  message="Skipped $what: missing ${missing[*]}."
  echo "::notice title=Skipped::$message"
  echo "- Skipped: $message" >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
  echo 'enabled=false' >> "$out"
else
  echo 'enabled=true' >> "$out"
fi
