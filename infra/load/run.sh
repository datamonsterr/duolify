#!/usr/bin/env bash
# Run the k6 load baseline against a throwaway compose project on ephemeral host ports.
# Env knobs (see baseline.js): READ_RPS, READ_DURATION, SSE_SECONDS, SSE_HEARTBEAT_SECONDS.
# LOAD_SUMMARY=<file> also writes the k6 summary JSON there.
set -euo pipefail
cd "$(dirname "$0")/../.."

port() { shuf -i 20000-39999 -n 1; }
export COMPOSE_PROJECT_NAME="duolify-load-$$"
for v in POSTGRES_PORT REDIS_PORT S3_PORT S3_CONSOLE_PORT API_PORT ENGINE_PORT MOCK_ENGINE_PORT; do
  export "$v=$(port)"
done
# Throwaway stack only; the API refuses secrets shorter than 32 bytes.
export JWT_SECRET=load-baseline-throwaway-not-a-secret-key
export API_URL="http://127.0.0.1:$API_PORT" MOCK_ENGINE_URL="http://127.0.0.1:$MOCK_ENGINE_PORT"

cleanup() { docker compose --profile '*' down --volumes --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker compose --profile load up -d --build --wait api mock-engine

args=()
[[ -n ${LOAD_SUMMARY:-} ]] && args+=(--summary-export "$LOAD_SUMMARY")
k6 run "${args[@]}" infra/load/baseline.js
