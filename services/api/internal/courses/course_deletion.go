package courses

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/jackc/pgx/v5"
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
	if !validIDs(id) {
		outlineError(w, 404)
		return
	}

	// Soft delete and active-course reset share one transaction: a failed reset must not leave
	// a hidden course as the learner's active course.
	tx, err := s.opts.Pool.Begin(r.Context())
	if err != nil {
		outlineError(w, 503)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	tag, err := tx.Exec(r.Context(), `
		UPDATE courses
		SET deleted_at = now()
		WHERE id = $1::uuid AND learner_id = $2::uuid AND deleted_at IS NULL
	`, id, owner)
	if err != nil {
		outlineError(w, 503)
		return
	}
	if tag.RowsAffected() == 0 {
		outlineError(w, 404)
		return
	}
	if _, err = tx.Exec(r.Context(), `
		UPDATE learner_profiles
		SET active_course_id = NULL
		WHERE learner_id = $1::uuid AND active_course_id = $2::uuid
	`, owner, id); err != nil {
		outlineError(w, 503)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		outlineError(w, 503)
		return
	}

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

	// Record the private generated-lesson object prefixes first: the cascade below removes the rows
	// that name them. The ledger row commits or rolls back with the delete.
	if _, err = tx.Exec(ctx, `
		INSERT INTO course_object_purges(prefix, purge_after)
		SELECT DISTINCT p, now() + make_interval(secs => $2) FROM (
			SELECT 'lessons/generated/' || g.id::text || '/' AS p
			FROM generated_lessons g JOIN courses c ON c.id = g.course_id
			WHERE c.deleted_at IS NOT NULL AND c.deleted_at <= $1
			UNION
			SELECT substring(m.manifest_key from '^lessons/generated/[0-9a-f-]{36}/')
			FROM lesson_manifests m
			JOIN course_lesson_stubs st ON st.id = m.lesson_id
			JOIN course_units u ON u.id = st.unit_id
			JOIN courses c ON c.id = u.course_id
			WHERE c.deleted_at IS NOT NULL AND c.deleted_at <= $1 AND m.manifest_key LIKE 'lessons/generated/%'
		) x WHERE p IS NOT NULL
		ON CONFLICT (prefix) DO NOTHING
	`, cutoff, ObjectPurgeRetention.Seconds()); err != nil {
		return 0, err
	}

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

// ObjectPurgeRetention keeps a purged Course's objects longer than the 14-day nightly dumps, so a
// restorable dump never points at deleted objects (same rule as shared snapshot retention).
const ObjectPurgeRetention = accounts.ObjectPurgeRetention

// ObjectStore lists and deletes stored lesson objects.
type ObjectStore interface {
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
}

// SweepPurgedObjects deletes the stored objects (slide HTML, manifests) of purged Courses once their
// retention has passed. A prefix that a live lesson manifest still names is skipped, never deleted.
// Rows are marked purged only after every object is gone, so a storage failure is retried next run.
// Published snapshots and narration audio are not touched: both have reference-counted retention.
func (s *Service) SweepPurgedObjects(ctx context.Context, now time.Time) (int, error) {
	if s.opts.Pool == nil || s.opts.Objects == nil {
		return 0, nil
	}
	rows, err := s.opts.Pool.Query(ctx, `SELECT p.prefix FROM course_object_purges p WHERE p.purged_at IS NULL AND p.purge_after <= $1 AND NOT EXISTS (SELECT 1 FROM lesson_manifests m WHERE starts_with(m.manifest_key, p.prefix)) ORDER BY p.purge_after LIMIT 200`, now)
	if err != nil {
		return 0, err
	}
	prefixes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	done := 0
	for _, prefix := range prefixes {
		var referenced bool
		if err = s.opts.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lesson_manifests WHERE starts_with(manifest_key, $1))`, prefix).Scan(&referenced); err != nil {
			return done, err
		}
		if referenced {
			continue
		}
		keys, err := s.opts.Objects.List(ctx, prefix)
		if err != nil {
			return done, err
		}
		for _, key := range keys {
			if err = s.opts.Objects.Delete(ctx, key); err != nil {
				return done, err
			}
		}
		if _, err = s.opts.Pool.Exec(ctx, `UPDATE course_object_purges SET purged_at = now() WHERE prefix = $1`, prefix); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}
