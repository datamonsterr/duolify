-- name: GetLearnerContext :one
SELECT (to_jsonb(p) - 'age_confirmed_at' - 'analytics_opt_out' - 'appearance' - 'motion' - 'narration' - 'narration_voice' - 'narration_speed' - 'captions' - 'text_size' - 'ui_sounds' - 'haptics' - 'fullscreen_on_rotate' - 'autoplay' - 'auto_send_after_dictation')::jsonb AS context
FROM learner_profiles p WHERE learner_id=sqlc.arg(learner_id)::uuid FOR UPDATE;

-- name: GetLearnerSettings :one
SELECT (to_jsonb(p) - 'age_confirmed_at' - 'narration_voice' - 'goal' - 'topic' - 'background' - 'level' - 'setup_completed')::jsonb AS context
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

-- name: SaveLearnerSettings :exec
UPDATE learner_profiles SET
 locale=sqlc.arg(context)::jsonb->>'locale',
 time_zone=sqlc.arg(context)::jsonb->>'time_zone',
 daily_minutes=(sqlc.arg(context)::jsonb->>'daily_minutes')::integer,
 daily_xp=(sqlc.arg(context)::jsonb->>'daily_xp')::integer,
 reminder_enabled=(sqlc.arg(context)::jsonb->>'reminder_enabled')::boolean,
 reminder_time=sqlc.arg(context)::jsonb->>'reminder_time',
 analytics_opt_out=(sqlc.arg(context)::jsonb->>'analytics_opt_out')::boolean,
 appearance=sqlc.arg(context)::jsonb->>'appearance',
 motion=sqlc.arg(context)::jsonb->>'motion',
 narration=(sqlc.arg(context)::jsonb->>'narration')::boolean,
 narration_speed=(sqlc.arg(context)::jsonb->>'narration_speed')::numeric,
 captions=(sqlc.arg(context)::jsonb->>'captions')::boolean,
 text_size=(sqlc.arg(context)::jsonb->>'text_size')::numeric,
 ui_sounds=(sqlc.arg(context)::jsonb->>'ui_sounds')::boolean,
 haptics=sqlc.arg(context)::jsonb->>'haptics',
 fullscreen_on_rotate=(sqlc.arg(context)::jsonb->>'fullscreen_on_rotate')::boolean,
 autoplay=(sqlc.arg(context)::jsonb->>'autoplay')::boolean,
 auto_send_after_dictation=(sqlc.arg(context)::jsonb->>'auto_send_after_dictation')::boolean,
 mascot=COALESCE(sqlc.arg(context)::jsonb->'mascot', mascot)
WHERE learner_id=sqlc.arg(learner_id)::uuid;
