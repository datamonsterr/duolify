#!/usr/bin/env bash
# E2E for infra:load-baseline: short variant against a throwaway compose stack.
# Asserts the public and authenticated read mix ran, and the Tutor chat stream went through the API,
# received heartbeats, finished with a done event and never went silent for 25 s.
set -euo pipefail
cd "$(dirname "$0")/../.."

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

LOAD_SUMMARY="$dir/k6.json" SSE_RESULT="$dir/sse.json" READ_DURATION=15s READ_RPS=5 \
  SSE_SECONDS=40 SSE_HEARTBEAT_INTERVAL=5s \
  bash infra/load/run.sh

reads=$(jq '.metrics.read_requests.count // 0' "$dir/k6.json")
authed=$(jq '.metrics.authed_read_requests.count // 0' "$dir/k6.json")
p95=$(jq '.metrics.http_req_duration["p(95)"]' "$dir/k6.json")
heartbeats=$(jq .heartbeats "$dir/sse.json")
gap=$(jq .max_gap_s "$dir/sse.json")
completed=$(jq .completed "$dir/sse.json")

[[ $reads -gt 0 && $authed -gt 0 ]] || { echo "FAIL: reads=$reads authed=$authed" >&2; exit 1; }
[[ $heartbeats -ge 4 ]] || { echo "FAIL: expected >=4 SSE heartbeats, got $heartbeats" >&2; exit 1; }
[[ $completed == true ]] || { echo "FAIL: no done event" >&2; exit 1; }
jq -e '.max_gap_s < 25' "$dir/sse.json" >/dev/null || { echo "FAIL: heartbeat gap $gap s" >&2; exit 1; }
echo "OK: reads=$reads authed=$authed p95=${p95}ms heartbeats=$heartbeats max_gap=${gap}s sse=$(jq -c "del(.tail)" "$dir/sse.json")"
