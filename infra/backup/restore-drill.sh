#!/usr/bin/env bash
# Input must be a decrypted, anonymized pg_dump --format=custom archive.
set -euo pipefail

if [[ $# != 1 || ! -r $1 ]]; then
  printf 'Usage: mise run infra:restore-drill -- /path/to/anonymized.dump\n' >&2
  exit 2
fi

drill_container=
cleanup() {
  if [[ -n $drill_container ]]; then
    docker rm -fv "$drill_container" >/dev/null
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# No published port or network access; only docker exec can reach the database.
drill_container=$(docker run --detach --network none \
  --env POSTGRES_HOST_AUTH_METHOD=trust \
  --env POSTGRES_DB=restore_drill postgres:18)

for attempt in {1..60}; do
  if docker exec "$drill_container" pg_isready -U postgres -d restore_drill >/dev/null 2>&1; then
    break
  fi
  if [[ $attempt == 60 ]]; then
    printf 'Throwaway Postgres did not become ready\n' >&2
    exit 1
  fi
  sleep 1
done

docker exec -i "$drill_container" pg_restore \
  --exit-on-error --no-owner --no-acl -U postgres -d restore_drill < "$1"

# Fail closed on absent migration metadata, failed Goose state, missing tables,
# or broken ledger relationships. Empty bootstrap tables remain valid.
docker exec -i "$drill_container" psql -X --set ON_ERROR_STOP=1 \
  -U postgres -d restore_drill <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM goose_db_version WHERE version_id > 0 AND is_applied) THEN
    RAISE EXCEPTION 'No applied application migration';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM river_migration) THEN
    RAISE EXCEPTION 'No applied River migration';
  END IF;
  IF EXISTS (
    SELECT 1 FROM credit_entries c LEFT JOIN learners l ON l.id = c.learner_id
    WHERE l.id IS NULL OR c.amount = 0
  ) THEN
    RAISE EXCEPTION 'Invalid credit ledger';
  END IF;
END $$;
SELECT count(*) AS learners_restored FROM learners;
SELECT count(*) AS credit_entries_restored, coalesce(sum(amount), 0) AS ledger_total FROM credit_entries;
SQL

printf 'Restore drill passed; disposable container will be removed.\n'
