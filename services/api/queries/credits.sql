-- name: CreateCreditEntry :one
INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
VALUES ($1, $2, $3, $4, $5, COALESCE($6, '{}'::jsonb)) RETURNING *;

-- name: ListCreditEntries :many
SELECT * FROM credit_entries WHERE learner_id = $1 ORDER BY id ASC;

-- name: ListCreditEntriesCursor :many
SELECT * FROM credit_entries
WHERE learner_id = $1 AND ($2::bigint = 0 OR id < $2::bigint)
ORDER BY id DESC
LIMIT $3;

-- name: GetDailyGrantForDate :one
SELECT * FROM credit_entries
WHERE learner_id = $1 AND kind = 'daily_grant' AND provenance->>'date' = $2::text
LIMIT 1;

-- name: GetLastDailyGrant :one
SELECT * FROM credit_entries
WHERE learner_id = $1 AND kind = 'daily_grant'
ORDER BY id DESC
LIMIT 1;

-- name: InsertCreditTimezone :one
INSERT INTO credit_timezones (learner_id, time_zone, effective_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetEffectiveCreditTimezone :one
SELECT time_zone, effective_at FROM credit_timezones
WHERE learner_id = $1 AND effective_at <= $2
ORDER BY effective_at DESC, id DESC
LIMIT 1;

-- name: GetPendingCreditTimezone :one
SELECT time_zone, effective_at FROM credit_timezones
WHERE learner_id = $1 AND effective_at > $2
ORDER BY effective_at ASC, id DESC
LIMIT 1;

-- name: ListUnexpiredDailyGrants :many
SELECT e.learner_id, (e.provenance->>'date')::text AS grant_date, e.amount, e.created_at
FROM credit_entries e
WHERE e.learner_id IS NOT NULL
  AND e.kind = 'daily_grant'
  AND NOT EXISTS (
      SELECT 1 FROM credit_entries x
      WHERE x.learner_id = e.learner_id
        AND x.kind = 'expire'
        AND x.provenance->>'date' = e.provenance->>'date'
  );
