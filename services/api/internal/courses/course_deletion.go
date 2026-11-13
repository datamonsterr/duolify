package courses

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

// ServeCourseDeleteHTTP allows an enrolled owner to soft-delete a course with immediate hiding.
func (s *Service) ServeCourseDeleteHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		outlineError(w, 401)
		return
	}
	if s.opts.Pool == nil {
		outlineError(w, 503)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		id = strings.TrimPrefix(r.URL.Path, "/v1/courses/")
	}

	tag, err := s.opts.Pool.Exec(r.Context(), `
		UPDATE courses
		SET deleted_at = now()
		WHERE id::text = $1 AND learner_id::text = $2 AND deleted_at IS NULL
	`, id, owner)
	if err != nil || tag.RowsAffected() == 0 {
		outlineError(w, 404)
		return
	}

	// Reset active course if the deleted course was active.
	_, _ = s.opts.Pool.Exec(r.Context(), `
		UPDATE learner_profiles
		SET active_course_id = NULL
		WHERE learner_id::text = $1 AND active_course_id::text = $2
	`, owner, id)

	respond(w, 200, map[string]any{
		"deleted": true,
		"id":      id,
	})
}

// PurgeDeletedCourses permanently purges courses soft-deleted older than the cutoff (30 days grace).
func (s *Service) PurgeDeletedCourses(ctx context.Context, cutoff time.Time) (int64, error) {
	if s.opts.Pool == nil {
		return 0, nil
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		DELETE FROM courses
		WHERE deleted_at IS NOT NULL AND deleted_at <= $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	purged := tag.RowsAffected()

	// Clean up unreferenced course_creations associated with purged courses
	_, err = tx.Exec(ctx, `
		DELETE FROM course_creations
		WHERE course_id IS NOT NULL AND course_id NOT IN (SELECT id FROM courses)
	`)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return purged, nil
}
