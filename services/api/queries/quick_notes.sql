-- name: UpsertQuickNote :one
INSERT INTO quick_notes (
    learner_id,
    action_key,
    course_id,
    lesson_id,
    slide_id,
    slide_title,
    source,
    raw_text,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (learner_id, action_key) DO NOTHING
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, reviewed_at, version, raw_version, created_at, updated_at;

-- name: GetQuickNoteByID :one
SELECT id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, reviewed_at, version, raw_version, created_at, updated_at
FROM quick_notes
WHERE id = $1 AND learner_id = $2;

-- name: ListQuickNotesByLearner :many
SELECT id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, reviewed_at, version, raw_version, created_at, updated_at
FROM quick_notes
WHERE learner_id = $1
ORDER BY created_at DESC;

-- name: CheckLessonAuthorization :one
SELECT EXISTS (
    SELECT 1
    FROM course_lesson_stubs l
    JOIN course_units u ON u.id = l.unit_id
    JOIN courses c ON c.id = u.course_id
    LEFT JOIN course_enrollments e ON e.course_id = c.id AND e.learner_id = $2
    WHERE l.id = $1 AND (l.learner_id = $2 OR c.learner_id = $2 OR e.learner_id = $2)
) AS authorized;

-- name: GetQuickNoteWithEnrichmentByID :one
SELECT 
    qn.id, qn.learner_id, qn.action_key, qn.course_id, qn.lesson_id, qn.slide_id, qn.slide_title, 
    qn.source, qn.raw_text, qn.status, qn.reviewed, qn.reviewed_at, qn.version, qn.raw_version, qn.reservation_id, qn.quote_id, qn.created_at, qn.updated_at,
    e.summary, e.tags, e.links, e.version AS enrichment_version, e.model AS enrichment_model, e.provider AS enrichment_provider
FROM quick_notes qn
LEFT JOIN quick_note_enrichments e ON e.note_id = qn.id
WHERE qn.id = $1 AND qn.learner_id = $2;

-- name: ListQuickNotesWithEnrichmentByLearner :many
SELECT 
    qn.id, qn.learner_id, qn.action_key, qn.course_id, qn.lesson_id, qn.slide_id, qn.slide_title, 
    qn.source, qn.raw_text, qn.status, qn.reviewed, qn.reviewed_at, qn.version, qn.raw_version, qn.reservation_id, qn.quote_id, qn.created_at, qn.updated_at,
    e.summary, e.tags, e.links, e.version AS enrichment_version, e.model AS enrichment_model, e.provider AS enrichment_provider
FROM quick_notes qn
LEFT JOIN quick_note_enrichments e ON e.note_id = qn.id
WHERE qn.learner_id = $1
ORDER BY qn.created_at DESC;

-- name: UpsertQuickNoteEnrichment :one
INSERT INTO quick_note_enrichments (
    note_id,
    learner_id,
    summary,
    tags,
    links,
    version,
    model,
    provider
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (note_id) DO UPDATE
SET summary = EXCLUDED.summary,
    tags = EXCLUDED.tags,
    links = EXCLUDED.links,
    version = EXCLUDED.version,
    model = EXCLUDED.model,
    provider = EXCLUDED.provider,
    updated_at = now()
RETURNING id, note_id, learner_id, summary, tags, links, version, model, provider, created_at, updated_at;

-- name: UpdateQuickNoteStatus :one
UPDATE quick_notes
SET status = $2,
    reservation_id = $3,
    quote_id = $4,
    updated_at = now()
WHERE id = $1 AND learner_id = $5
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, reviewed_at, version, raw_version, reservation_id, quote_id, created_at, updated_at;

-- name: ListPendingNotesForLearner :many
SELECT id, learner_id, raw_version, version, created_at
FROM quick_notes
WHERE learner_id = $1 AND status = 'enrichment_pending'
ORDER BY created_at ASC
FOR UPDATE SKIP LOCKED;

-- name: GetQuickNoteForUpdate :one
SELECT id, learner_id, course_id, lesson_id, slide_title, raw_text, status, raw_version, version, reservation_id, quote_id
FROM quick_notes
WHERE id = $1 AND learner_id = $2
FOR UPDATE;

-- name: ListCourseLessonsForCourse :many
SELECT l.id, l.title
FROM course_lesson_stubs l
JOIN course_units u ON u.id = l.unit_id
JOIN courses c ON c.id=u.course_id
WHERE u.course_id = $1 AND c.deleted_at IS NULL
  AND (l.learner_id=$2 OR (l.learner_id=c.learner_id AND EXISTS (SELECT 1 FROM course_enrollments e WHERE e.course_id=c.id AND e.learner_id=$2)))
ORDER BY u.position, l.position;

-- name: GetCourseIDForLesson :one
SELECT u.course_id
FROM course_lesson_stubs l
JOIN course_units u ON u.id = l.unit_id
JOIN courses c ON c.id=u.course_id
WHERE l.id = $1 AND c.deleted_at IS NULL
  AND (c.learner_id=$2 OR EXISTS (SELECT 1 FROM course_enrollments e WHERE e.course_id=c.id AND e.learner_id=$2));

-- name: UpdateQuickNoteRawText :one
UPDATE quick_notes
SET raw_text = $3,
    version = version + 1,
    raw_version = raw_version + 1,
    status = 'enrichment_pending',
    updated_at = now()
WHERE id = $1 AND learner_id = $2 AND version = $4 AND $4 >= 1
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, reviewed_at, version, raw_version, created_at, updated_at;

-- name: MarkQuickNoteReviewed :one
UPDATE quick_notes
SET reviewed = true,
    reviewed_at = coalesce(reviewed_at, now()),
    updated_at = now()
WHERE id = $1 AND learner_id = $2
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, reviewed_at, version, raw_version, created_at, updated_at;

-- name: DeleteQuickNote :exec
DELETE FROM quick_notes
WHERE id = $1 AND learner_id = $2;

-- name: FilterQuickNotes :many
SELECT 
    qn.id, qn.learner_id, qn.action_key, qn.course_id, qn.lesson_id, qn.slide_id, qn.slide_title, 
    qn.source, qn.raw_text, qn.status, qn.reviewed, qn.reviewed_at, qn.version, qn.raw_version, qn.reservation_id, qn.quote_id, qn.created_at, qn.updated_at,
    e.summary, e.tags, e.links, e.version AS enrichment_version, e.model AS enrichment_model, e.provider AS enrichment_provider
FROM quick_notes qn
LEFT JOIN quick_note_enrichments e ON e.note_id = qn.id
WHERE qn.learner_id = sqlc.arg('learner_id')
  AND (sqlc.narg('course_id')::uuid IS NULL OR qn.course_id = sqlc.narg('course_id'))
  AND (sqlc.narg('lesson_id')::uuid IS NULL OR qn.lesson_id = sqlc.narg('lesson_id'))
  AND (sqlc.narg('reviewed')::boolean IS NULL OR qn.reviewed = sqlc.narg('reviewed'))
  AND (sqlc.narg('status')::text IS NULL OR qn.status = sqlc.narg('status'))
  AND (sqlc.narg('tag')::text IS NULL OR length(trim(sqlc.narg('tag'))) = 0 OR EXISTS (
      SELECT 1 FROM unnest(e.tags) t WHERE lower(t) = lower(sqlc.narg('tag'))
  ))
  AND (
    sqlc.narg('search_query')::text IS NULL OR length(trim(sqlc.narg('search_query'))) = 0
    OR qn.raw_text ILIKE '%' || sqlc.narg('search_query') || '%'
    OR qn.slide_title ILIKE '%' || sqlc.narg('search_query') || '%'
    OR (e.summary IS NOT NULL AND e.summary ILIKE '%' || sqlc.narg('search_query') || '%')
    OR (e.tags IS NOT NULL AND EXISTS (
        SELECT 1 FROM unnest(e.tags) t WHERE t ILIKE '%' || sqlc.narg('search_query') || '%'
    ))
  )
ORDER BY qn.created_at DESC;

-- name: GetQuickNoteByActionKey :one
SELECT id FROM quick_notes WHERE learner_id=$1 AND action_key=$2;

-- name: CheckNoteCourseAuthorization :one
SELECT EXISTS(SELECT 1 FROM courses c WHERE c.id=$1 AND c.deleted_at IS NULL
 AND (c.learner_id=$2 OR EXISTS(SELECT 1 FROM course_enrollments e WHERE e.course_id=c.id AND e.learner_id=$2)));
