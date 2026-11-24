-- +goose Up
CREATE SCHEMA edge_public;
REVOKE ALL ON SCHEMA edge_public FROM PUBLIC;

-- Keep visibility predicates inside a security barrier. The search document is derived from the
-- same screened public title, tags and description used by the Go Marketplace.
CREATE VIEW edge_public.marketplace_browse_v1 WITH (security_barrier = true) AS
SELECT l.course_id, l.title, l.description, l.category, l.language, l.tags, l.version, l.published_at,
       l.generated_lessons AS lessons_ready, l.lesson_count AS lessons_total,
       jsonb_build_object('username', COALESCE(lu.username, ''), 'display_name', COALESCE(o.display_name, '')) AS creator,
       (SELECT count(*) FROM public.course_copies cc WHERE cc.source_course_id = l.course_id) AS enrollment_count,
       st.rating_sum, st.rating_count, st.popular_score, st.enrollments_30d, l.search AS search_document
FROM public.marketplace_listings l
JOIN public.marketplace_listing_stats st ON st.course_id = l.course_id
JOIN public.courses c ON c.id = l.course_id
JOIN public.learners o ON o.id = l.owner_id
LEFT JOIN public.learner_usernames lu ON lu.learner_id = l.owner_id
WHERE l.delisted_at IS NULL AND l.hidden_at IS NULL AND c.deleted_at IS NULL AND c.visibility = 'public';

-- Go uses this aggregate as the platform mean for Bayesian ordering.
CREATE VIEW edge_public.marketplace_rating_v1 AS
SELECT sum(st.rating_sum)::float8 / NULLIF(sum(st.rating_count), 0) AS mean
FROM public.marketplace_listing_stats st
JOIN public.marketplace_listings l ON l.course_id = st.course_id
WHERE l.delisted_at IS NULL AND l.hidden_at IS NULL;

-- Project only the public outline and up to three slides from generated lessons in order.
-- The view never exposes the publication snapshot, identifiers of its owner, or lesson answers.
CREATE VIEW edge_public.marketplace_course_v1 WITH (security_barrier = true) AS
SELECT b.course_id, b.title, b.description, b.category, b.language, COALESCE(b.tags, ARRAY[]::text[]) AS tags,
       b.version, b.published_at, b.creator, b.lessons_ready, b.lessons_total, b.enrollment_count,
       b.rating_sum, b.rating_count, preview.outline, samples.sample_slides
FROM edge_public.marketplace_browse_v1 b
JOIN public.marketplace_listings l ON l.course_id = b.course_id
JOIN LATERAL (
  SELECT d.snapshot
  FROM public.publication_drafts d
  WHERE d.id = l.draft_id OR (d.course_id = b.course_id AND d.number = b.version AND d.verdict = 'pass')
  ORDER BY d.number DESC
  LIMIT 1
) d ON true
CROSS JOIN LATERAL (
  SELECT COALESCE(jsonb_agg(jsonb_build_object(
    'title', COALESCE(u.unit->>'title', ''),
    'lessons', (
      SELECT COALESCE(jsonb_agg(jsonb_build_object(
        'title', COALESCE(le.lesson->>'title', ''),
        'generated', COALESCE((le.lesson->>'generated')::boolean, false)
      ) ORDER BY le.position), '[]'::jsonb)
      FROM jsonb_array_elements(NULLIF(u.unit->'lessons', 'null'::jsonb)) WITH ORDINALITY le(lesson, position)
    )
  ) ORDER BY u.position), '[]'::jsonb) AS outline
  FROM jsonb_array_elements(NULLIF(d.snapshot->'units', 'null'::jsonb)) WITH ORDINALITY u(unit, position)
) preview
CROSS JOIN LATERAL (
  SELECT jsonb_agg(slide ORDER BY unit_position, lesson_position, slide_position) AS sample_slides
  FROM (
    SELECT jsonb_build_object('id', COALESCE(sl.slide->>'id', ''), 'html', COALESCE(sl.slide->>'html', '')) AS slide,
           u.position AS unit_position, le.position AS lesson_position, sl.position AS slide_position
    FROM jsonb_array_elements(NULLIF(d.snapshot->'units', 'null'::jsonb)) WITH ORDINALITY u(unit, position)
    CROSS JOIN LATERAL jsonb_array_elements(NULLIF(u.unit->'lessons', 'null'::jsonb)) WITH ORDINALITY le(lesson, position)
    CROSS JOIN LATERAL jsonb_array_elements(NULLIF(le.lesson->'slides', 'null'::jsonb)) WITH ORDINALITY sl(slide, position)
    WHERE (le.lesson->>'generated')::boolean
    ORDER BY u.position, le.position, sl.position
    LIMIT 3
  ) selected
) samples;

-- +goose Down
DROP VIEW edge_public.marketplace_course_v1;
DROP VIEW edge_public.marketplace_rating_v1;
DROP VIEW edge_public.marketplace_browse_v1;
DROP SCHEMA edge_public;
