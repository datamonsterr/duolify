#!/usr/bin/env bash
# Load baseline on a throwaway compose project and ephemeral host ports (E18-02).
# Stack: Postgres, Redis, object store, real API; the Engine is a mock gRPC server behind the API
# (infra/load/compose.yml). Seeds one dev learner through the real sign-in route, then runs the k6
# read mix and the Tutor chat stream (through the API) at the same time.
# Knobs: READ_RPS, READ_DURATION, SSE_SECONDS, SSE_HEARTBEAT_INTERVAL, MAX_GAP_SECONDS,
# LOAD_SUMMARY=<file> (k6 summary JSON), SSE_RESULT=<file> (stream leg JSON).
set -euo pipefail
cd "$(dirname "$0")/../.."

port() { shuf -i 20000-39999 -n 1; }
export COMPOSE_PROJECT_NAME="duolify-load-$$"
for v in POSTGRES_PORT REDIS_PORT S3_PORT S3_CONSOLE_PORT API_PORT; do export "$v=$(port)"; done
# Throwaway stack only; the API refuses secrets shorter than 32 bytes.
export JWT_SECRET=load-baseline-throwaway-not-a-secret-key
export SSE_SECONDS="${SSE_SECONDS:-600}" SSE_HEARTBEAT_INTERVAL="${SSE_HEARTBEAT_INTERVAL:-15s}"
export API_URL="http://127.0.0.1:$API_PORT"
dc() { docker compose -f docker-compose.yml -f infra/load/compose.yml "$@"; }

cleanup() { [[ -n ${LOAD_KEEP:-} ]] && return; dc down --volumes --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Seed commands run on the host against the published ports (dev defaults from docker-compose.yml).
seed() {
  (cd services/api && DUOLIFY_ENV=development \
    DATABASE_URL="postgres://duolify:duolify@127.0.0.1:$POSTGRES_PORT/duolify?sslmode=disable" \
    S3_ENDPOINT="http://127.0.0.1:$S3_PORT" S3_BUCKET=duolify S3_REGION=us-east-1 \
    S3_ACCESS_KEY_ID=duolify S3_SECRET_ACCESS_KEY=duolify-dev-secret go run ./cmd/loadseed "$@")
}

# Tutor replies are capped at 30 s by the launch pricing catalogue; raise it for this database only.
dc up -d --build --wait postgres mock-engine
dc run --rm migrate >/dev/null
seed pricing $((SSE_SECONDS + 120))
dc up -d --build --wait api

# Sign in through the real routes; the dev mailer logs the code.
email="load-baseline@example.test"
curl -fsS -X POST "$API_URL/v1/auth/code" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$email\",\"age_confirmed\":true}" >/dev/null
code=""
for _ in $(seq 1 20); do
  code=$(dc logs api 2>&1 | grep 'DEV ONLY sign-in mail' | grep -o '"\?code"\?[=:] *"\?[0-9]\{6\}' | grep -o '[0-9]\{6\}' | tail -1)
  [[ -n $code ]] && break
  sleep 1
done
[[ -n $code ]] || { echo "no sign-in code in API log" >&2; exit 1; }
export ACCESS_TOKEN
ACCESS_TOKEN=$(curl -fsS -X POST "$API_URL/v1/auth/verify" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$email\",\"age_confirmed\":true,\"code\":\"$code\"}" | jq -r .access_token)
owner=$(cut -d. -f2 <<<"$ACCESS_TOKEN" | tr '_-' '/+' | base64 -d 2>/dev/null | jq -r .sub)
fixtures=$(seed fixtures "$owner" | tail -1)
export COURSE_ID LESSON_ID
COURSE_ID=$(jq -r .course <<<"$fixtures")
LESSON_ID=$(jq -r .lesson <<<"$fixtures")

# The access token lives 15 minutes; the longest run (10 min stream) fits inside it.
export READ_DURATION="${READ_DURATION:-10m}"
export SSE_RESULT="${SSE_RESULT:-$(mktemp)}"
node infra/load/sse-stream.mjs & sse=$!
args=()
[[ -n ${LOAD_SUMMARY:-} ]] && args+=(--summary-export "$LOAD_SUMMARY")
k6_status=0
k6 run "${args[@]}" infra/load/baseline.js || k6_status=$?
sse_status=0
wait "$sse" || sse_status=$?
[[ $k6_status -eq 0 && $sse_status -eq 0 ]] || { echo "FAIL: k6=$k6_status sse-stream=$sse_status" >&2; exit 1; }
