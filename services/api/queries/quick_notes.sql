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
ON CONFLICT (learner_id, action_key) DO UPDATE
SET updated_at = quick_notes.updated_at
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, raw_version, created_at, updated_at;

-- name: GetQuickNoteByID :one
SELECT id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, raw_version, created_at, updated_at
FROM quick_notes
WHERE id = $1 AND learner_id = $2;

-- name: ListQuickNotesByLearner :many
SELECT id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, raw_version, created_at, updated_at
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
    qn.source, qn.raw_text, qn.status, qn.reviewed, qn.raw_version, qn.reservation_id, qn.quote_id, qn.created_at, qn.updated_at,
    e.summary, e.tags, e.links, e.version AS enrichment_version, e.model AS enrichment_model, e.provider AS enrichment_provider
FROM quick_notes qn
LEFT JOIN quick_note_enrichments e ON e.note_id = qn.id
WHERE qn.id = $1 AND qn.learner_id = $2;

-- name: ListQuickNotesWithEnrichmentByLearner :many
SELECT 
    qn.id, qn.learner_id, qn.action_key, qn.course_id, qn.lesson_id, qn.slide_id, qn.slide_title, 
    qn.source, qn.raw_text, qn.status, qn.reviewed, qn.raw_version, qn.reservation_id, qn.quote_id, qn.created_at, qn.updated_at,
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
WHERE id = $1
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, raw_version, reservation_id, quote_id, created_at, updated_at;

-- name: UpdateQuickNoteRawText :one
UPDATE quick_notes
SET raw_text = $2,
    raw_version = raw_version + 1,
    status = 'enrichment_pending',
    updated_at = now()
WHERE id = $1 AND learner_id = $3
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, raw_version, created_at, updated_at;

-- name: ListPendingNotesForLearner :many
SELECT id, learner_id, raw_version, created_at
FROM quick_notes
WHERE learner_id = $1 AND status = 'enrichment_pending'
ORDER BY created_at ASC
FOR UPDATE SKIP LOCKED;

-- name: GetQuickNoteForUpdate :one
SELECT id, learner_id, course_id, lesson_id, slide_title, raw_text, status, raw_version, reservation_id, quote_id
FROM quick_notes
WHERE id = $1
FOR UPDATE;

-- name: ListCourseLessonsForCourse :many
SELECT l.id, l.title
FROM course_lesson_stubs l
JOIN course_units u ON u.id = l.unit_id
WHERE u.course_id = $1
ORDER BY u.position, l.position;

-- name: GetCourseIDForLesson :one
SELECT u.course_id
FROM course_lesson_stubs l
JOIN course_units u ON u.id = l.unit_id
WHERE l.id = $1;
