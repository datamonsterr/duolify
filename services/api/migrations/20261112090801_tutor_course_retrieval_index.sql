-- +goose Up
-- Derived, rebuildable full-text index over lesson slide text for tutor retrieval (E8-03).
-- The expression must match services/api/internal/chat/retrieve.go exactly. No new data is stored:
-- after a restore, REINDEX INDEX lesson_slides_fts rebuilds it from the backed-up slides.
CREATE INDEX lesson_slides_fts ON lesson_slides USING gin (
  to_tsvector('simple'::regconfig, narration || ' ' || regexp_replace(html, '<[^>]*>', ' ', 'g'))
);

-- +goose Down
DROP INDEX IF EXISTS lesson_slides_fts;
