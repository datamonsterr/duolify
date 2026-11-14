#!/usr/bin/env bash
# Scheduled real-data restore drill. Skips with a notice (exit 0) unless the
# owner's restorer credentials and age identity are present. The decrypted dump
# is restored only into a throwaway Postgres published on loopback, and
# anonymized by cmd/restore-drill before any check touches it.
set -euo pipefail

for variable in BACKUP_S3_ENDPOINT BACKUP_S3_BUCKET BACKUP_RESTORE_S3_ACCESS_KEY_ID BACKUP_RESTORE_S3_SECRET_ACCESS_KEY BACKUP_AGE_IDENTITY; do
  if [[ -z ${!variable:-} ]]; then
    printf 'Restore drill skipped: %s is not configured (owner credentials required).\n' "$variable"
    exit 0
  fi
done

if [[ ${DRILL_FINANCIAL_REPLAY:-false} == true ]]; then
  for variable in BACKUP_FINANCIAL_FIXTURES BACKUP_FINANCIAL_FIXTURE_SIGNATURE BACKUP_FINANCIAL_FIXTURE_KEY; do
    if [[ -z ${!variable:-} ]]; then
      printf 'Financial replay skipped: %s is not configured (owner-supplied signed TEST fixtures required).\n' "$variable"
      exit 0
    fi
  done
fi

pg_port=${DRILL_PG_PORT:-4112}
redis_port=${DRILL_REDIS_PORT:-4113}
name="duolify-drill-$$"
cleanup() { docker rm -fv "$name-pg" "$name-redis" >/dev/null 2>&1 || true; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker run --detach --name "$name-pg" --publish "127.0.0.1:$pg_port:5432" \
  --env POSTGRES_HOST_AUTH_METHOD=trust --env POSTGRES_DB=postgres postgres:18 >/dev/null
docker run --detach --name "$name-redis" --publish "127.0.0.1:$redis_port:6379" redis:8 >/dev/null
for attempt in {1..60}; do
  if docker exec "$name-pg" pg_isready -U postgres >/dev/null 2>&1 && docker exec "$name-redis" redis-cli ping >/dev/null 2>&1; then
    break
  fi
  if [[ $attempt == 60 ]]; then
    printf 'Throwaway Postgres/Redis did not become ready\n' >&2
    exit 1
  fi
  sleep 1
done

cd "${MISE_PROJECT_ROOT:-.}/services/api"
DRILL_PG_CONTAINER="$name-pg" \
  DRILL_DATABASE_URL="postgres://postgres@127.0.0.1:$pg_port/postgres?sslmode=disable" \
  DRILL_REDIS_URL="redis://127.0.0.1:$redis_port/0" \
  go run ./cmd/restore-drill
