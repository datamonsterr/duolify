#!/usr/bin/env bash
# E2E for infra:load-baseline: short variant against a throwaway compose stack.
# Asserts the read mix ran and the mock-Engine Tutor SSE stream finished with heartbeats.
set -euo pipefail
cd "$(dirname "$0")/../.."

summary=$(mktemp)
trap 'rm -f "$summary"' EXIT

LOAD_SUMMARY="$summary" READ_DURATION=15s READ_RPS=5 SSE_SECONDS=30 SSE_HEARTBEAT_SECONDS=10 \
  bash infra/load/run.sh

reads=$(jq '.metrics.read_requests.count // 0' "$summary")
heartbeats=$(jq '.metrics.sse_heartbeats.count // 0' "$summary")
done_events=$(jq '.metrics.sse_done.count // 0' "$summary")

[[ $reads -gt 0 ]] || { echo "FAIL: no read-mix requests recorded" >&2; exit 1; }
[[ $heartbeats -ge 2 ]] || { echo "FAIL: expected >=2 SSE heartbeats, got $heartbeats" >&2; exit 1; }
[[ $done_events -eq 1 ]] || { echo "FAIL: expected one SSE done event, got $done_events" >&2; exit 1; }
echo "OK: reads=$reads heartbeats=$heartbeats done=$done_events"
