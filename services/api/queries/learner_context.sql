-- name: GetLearnerContext :one
SELECT (to_jsonb(p) - 'age_confirmed_at' - 'analytics_opt_out')::jsonb AS context
FROM learner_profiles p WHERE learner_id=sqlc.arg(learner_id)::uuid FOR UPDATE;

-- name: SaveLearnerContext :exec
UPDATE learner_profiles SET
 goal=sqlc.arg(context)::jsonb->>'goal',
 topic=sqlc.arg(context)::jsonb->>'topic',
 background=sqlc.arg(context)::jsonb->>'background',
 level=sqlc.arg(context)::jsonb->>'level',
 locale=sqlc.arg(context)::jsonb->>'locale',
 time_zone=sqlc.arg(context)::jsonb->>'time_zone',
 daily_minutes=(sqlc.arg(context)::jsonb->>'daily_minutes')::integer,
 daily_xp=(sqlc.arg(context)::jsonb->>'daily_xp')::integer,
 reminder_enabled=(sqlc.arg(context)::jsonb->>'reminder_enabled')::boolean,
 reminder_time=sqlc.arg(context)::jsonb->>'reminder_time',
 setup_completed=true
WHERE learner_id=sqlc.arg(learner_id)::uuid;
