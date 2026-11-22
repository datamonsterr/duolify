-- name: InsertCourseBrief :one
INSERT INTO course_brief_revisions(learner_id,brief,content_hash)
VALUES(sqlc.arg(learner_id)::uuid,sqlc.arg(brief)::jsonb,sqlc.arg(content_hash)::text)
RETURNING id::text,brief,content_hash,created_at,expires_at;

-- name: GetCourseBrief :one
SELECT id::text,brief,content_hash,created_at,expires_at FROM course_brief_revisions
WHERE id=sqlc.arg(id)::text::uuid AND learner_id=sqlc.arg(learner_id)::text::uuid AND expires_at>now();

-- name: PurgeExpiredCourseBriefs :exec
DELETE FROM course_brief_revisions WHERE expires_at<=now();
