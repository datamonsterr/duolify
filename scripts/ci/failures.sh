#!/usr/bin/env bash
# Usage: failures.sh [PR number | run id]   (default: PR of the current branch)
# Prints failing jobs, failing steps, check annotations and the tail of each
# failing job log, also while the run is still in progress.
set -euo pipefail
arg=${1:-}
repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)

if [[ -z $arg ]]; then
  arg=$(gh pr view --json number --jq .number)
fi
if [[ $arg =~ ^[0-9]{9,}$ ]]; then
  runs=$arg
else
  sha=$(gh pr view "$arg" --json headRefOid --jq .headRefOid)
  runs=$(gh run list --commit "$sha" --json databaseId --jq '.[].databaseId')
fi

found=0
for run in $runs; do
  gh run view "$run" --json workflowName,status,conclusion,url \
    --jq '"== run \(.workflowName) \(.status)/\(.conclusion) \(.url)"'
  jobs=$(gh run view "$run" --json jobs --jq '
    .jobs[] | select(.conclusion == "failure" or .conclusion == "cancelled"
      or ([.steps[]? | select(.conclusion == "failure")] | length > 0))
    | "\(.databaseId)\t\(.name)"')
  while IFS=$'\t' read -r id name; do
    [[ -n ${id:-} ]] || continue
    found=1
    echo "---- FAILED job: $name (id $id)"
    gh api "repos/$repo/actions/jobs/$id" \
      --jq '.steps[] | select(.conclusion == "failure") | "  step \(.number): \(.name)"'
    gh api "repos/$repo/check-runs/$id/annotations" \
      --jq '.[] | select(.annotation_level != "notice") | "  \(.annotation_level) \(.path):\(.start_line) \(.title // "") \(.message)"' || true
    echo "  -- log around the first errors (also: gh run view $run --job $id --log-failed once the run is done):"
    gh api --allow-escape-sequences "repos/$repo/actions/jobs/$id/logs" 2>&1 \
      | sed -e 's/\x1b\[[0-9;]*m//g' -e 's/^[0-9TZ:.-]* //' \
      | grep -m1 -B30 -A2 '##\[error\]' | tail -n 40 | sed 's/^/  | /' || true
  done <<< "$jobs"
done
[[ $found == 1 ]] || echo "No failing jobs."
