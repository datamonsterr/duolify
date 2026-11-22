package courses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/datamonsterr/duolify/services/api/internal/httpcache"
	"io"
	"net/http"
	"strings"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

// ValidateCourseLevel ensures level overrides adhere to allowed profile levels.
func ValidateCourseLevel(level string) error {
	switch level {
	case "", "beginner", "intermediate", "advanced", "expert":
		return nil
	default:
		return fmt.Errorf("invalid course level: %s", level)
	}
}

// ProjectEffectiveProfile creates an engine LearnerProfile overlaying any course-specific level.
func ProjectEffectiveProfile(base *enginev1.LearnerProfile, courseLevelOverride string) *enginev1.LearnerProfile {
	var eff *enginev1.LearnerProfile
	if base != nil {
		eff = proto.Clone(base).(*enginev1.LearnerProfile)
	} else {
		eff = new(enginev1.LearnerProfile)
	}
	if courseLevelOverride != "" {
		eff.Level = courseLevelOverride
	}
	return eff
}

// EffectiveProfile loads global learner context and applies course level override without overwriting global profile.
func (s *Service) EffectiveProfile(ctx context.Context, learnerID, courseID string) (*enginev1.LearnerProfile, error) {
	learner := &enginev1.LearnerProfile{LearnerId: learnerID}
	if s.opts.Pool == nil {
		return learner, nil
	}
	err := s.opts.Pool.QueryRow(ctx, `SELECT locale, background, goal, level, topic, time_zone, COALESCE(daily_minutes,0), COALESCE(daily_xp,0), COALESCE(reminder_enabled,false), reminder_time FROM learner_profiles WHERE learner_id::text=$1`, learnerID).Scan(
		&learner.Locale,
		&learner.Background,
		&learner.Goals,
		&learner.Level,
		&learner.Topic,
		&learner.TimeZone,
		&learner.DailyMinutes,
		&learner.DailyXp,
		&learner.ReminderEnabled,
		&learner.ReminderTime,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if courseID != "" {
		var override string
		err = s.opts.Pool.QueryRow(ctx, `SELECT COALESCE(level_override, '') FROM course_enrollments WHERE learner_id::text=$1 AND course_id::text=$2`, learnerID, courseID).Scan(&override)
		if err == nil && override != "" {
			return ProjectEffectiveProfile(learner, override), nil
		}
	}
	return learner, nil
}

// ServeCoursesListHTTP returns a summary of every confirmed course the authenticated learner is
// enrolled in: what a course card needs, with progress counted in SQL and no unit or lesson
// tree. The tree comes from GET /v1/courses/{id}. The response is per learner (private,
// revalidated) with an ETag so an unchanged list is a 304.
func (s *Service) ServeCoursesListHTTP(w http.ResponseWriter, r *http.Request) {
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

	var activeCourseID string
	// No profile row means no active course; a database failure must not report one as unset.
	if err := s.opts.Pool.QueryRow(r.Context(), `SELECT COALESCE(active_course_id::text, '') FROM learner_profiles WHERE learner_id=$1::uuid`, owner).Scan(&activeCourseID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		outlineError(w, 503)
		return
	}

	var coursesRaw []byte
	err := s.opts.Pool.QueryRow(r.Context(), courseSummariesSQL, owner).Scan(&coursesRaw)
	if err != nil {
		outlineError(w, 500)
		return
	}

	var items []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(coursesRaw, &items)
	found := false
	for _, item := range items {
		if item.ID == activeCourseID {
			found = true
			break
		}
	}
	if !found {
		activeCourseID = ""
		if len(items) > 0 {
			activeCourseID = items[0].ID
		}
	}

	httpcache.WriteJSON(w, r, 200, httpcache.Private, map[string]any{
		"courses":          json.RawMessage(coursesRaw),
		"active_course_id": activeCourseID,
	})
}

// courseSummariesSQL builds one JSON array of course summaries, newest activity first. Progress
// is aggregated per course in a single pass over the lesson stubs, so the statement count and the
// payload do not grow with lessons per course. unit_index and lesson_index are positions among
// the non-archived units and the lessons of that unit; current is null when no lesson is current.
const courseSummariesSQL = `
	WITH mine AS (
		SELECT c.id, c.title, c.description, COALESCE(e.level_override, '') AS level_override, e.last_active_at, e.created_at
		FROM courses c JOIN course_enrollments e ON e.course_id=c.id
		WHERE e.learner_id=$1::uuid AND c.deleted_at IS NULL
	), ranked_units AS (
		SELECT u.id, u.course_id, row_number() OVER (PARTITION BY u.course_id ORDER BY u.position) - 1 AS ui
		FROM course_units u WHERE u.course_id IN (SELECT id FROM mine) AND u.archived_at IS NULL
	), lessons AS (
		SELECT ru.course_id, ru.ui, l.status,
			row_number() OVER (PARTITION BY l.unit_id ORDER BY l.position) - 1 AS li,
			count(*) OVER (PARTITION BY l.unit_id) AS unit_lessons
		FROM course_lesson_stubs l JOIN ranked_units ru ON ru.id=l.unit_id
	), progress AS (
		SELECT course_id, count(*) AS total, count(*) FILTER (WHERE status='done') AS done,
			(array_agg(jsonb_build_object('unit_index', ui, 'lesson_index', li, 'unit_lessons', unit_lessons) ORDER BY ui, li) FILTER (WHERE status='current'))[1] AS cur
		FROM lessons GROUP BY course_id
	)
	SELECT COALESCE(jsonb_agg(
		jsonb_build_object(
			'id', m.id,
			'title', m.title,
			'description', m.description,
			'level_override', m.level_override,
			'pinned_version', (SELECT cc.version FROM course_copies cc WHERE cc.copy_course_id=m.id),
			'moderation_blocked', EXISTS(SELECT 1 FROM course_copies bc WHERE bc.copy_course_id=m.id AND bc.blocked_at IS NOT NULL),
			'copy_origin', (SELECT jsonb_build_object('creator_username', u.username, 'creator_deleted', cc.source_owner_id IS NULL, 'source_available', EXISTS(SELECT 1 FROM courses sc WHERE sc.id=cc.source_course_id AND sc.deleted_at IS NULL AND sc.visibility<>'private')) FROM course_copies cc LEFT JOIN learner_usernames u ON u.learner_id=cc.source_owner_id WHERE cc.copy_course_id=m.id),
			'progress', jsonb_build_object('lessons_total', COALESCE(p.total, 0), 'lessons_done', COALESCE(p.done, 0), 'current', p.cur)
		) ORDER BY m.last_active_at DESC, m.created_at DESC
	), '[]'::jsonb)
	FROM mine m LEFT JOIN progress p ON p.course_id=m.id`

// ServeCourseSelectHTTP updates the active course for an enrolled learner.
func (s *Service) ServeCourseSelectHTTP(w http.ResponseWriter, r *http.Request) {
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

	courseID := r.PathValue("id")
	if courseID == "" {
		courseID = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/courses/"), "/select")
	}

	ctx := r.Context()
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		outlineError(w, 503)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Lock the course against concurrent soft deletion through commit.
	var locked string
	err = tx.QueryRow(ctx, `SELECT c.id::text FROM courses c JOIN course_enrollments e ON e.course_id=c.id WHERE e.learner_id::text=$1 AND c.id::text=$2 AND c.deleted_at IS NULL FOR UPDATE OF c`, owner, courseID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		outlineError(w, 404)
		return
	}
	if err != nil {
		outlineError(w, 503)
		return
	}
	tag, err := tx.Exec(ctx, `UPDATE course_enrollments SET last_active_at=now() WHERE learner_id::text=$1 AND course_id::text=$2`, owner, courseID)
	if err != nil {
		outlineError(w, 503)
		return
	}
	if tag.RowsAffected() != 1 {
		outlineError(w, 404)
		return
	}
	tag, err = tx.Exec(ctx, `UPDATE learner_profiles SET active_course_id=$2::uuid WHERE learner_id::text=$1`, owner, courseID)
	if err != nil || tag.RowsAffected() != 1 {
		outlineError(w, 503)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		outlineError(w, 503)
		return
	}

	respond(w, 200, map[string]any{
		"active_course_id": courseID,
	})
}

// ServeCourseLevelHTTP updates or resets the per-course level override without modifying global profile.
func (s *Service) ServeCourseLevelHTTP(w http.ResponseWriter, r *http.Request) {
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

	courseID := r.PathValue("id")
	if courseID == "" {
		courseID = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/courses/"), "/level")
	}

	var in struct {
		Level *string `json:"level"`
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || in.Level == nil {
		outlineError(w, 400)
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		outlineError(w, 400)
		return
	}
	level := *in.Level
	if ValidateCourseLevel(level) != nil {
		outlineError(w, 400)
		return
	}
	ctx := r.Context()
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		outlineError(w, 503)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked string
	err = tx.QueryRow(ctx, `SELECT c.id::text FROM courses c JOIN course_enrollments e ON e.course_id=c.id WHERE e.learner_id::text=$1 AND c.id::text=$2 AND c.deleted_at IS NULL FOR UPDATE OF c`, owner, courseID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		outlineError(w, 404)
		return
	}
	if err != nil {
		outlineError(w, 503)
		return
	}
	tag, err := tx.Exec(ctx, `UPDATE course_enrollments SET level_override=$3 WHERE learner_id::text=$1 AND course_id::text=$2`, owner, courseID, level)
	if err != nil {
		outlineError(w, 503)
		return
	}
	if tag.RowsAffected() != 1 {
		outlineError(w, 404)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		outlineError(w, 503)
		return
	}

	var optOut bool
	if s.opts.Pool.QueryRow(r.Context(), `SELECT analytics_opt_out FROM learner_profiles WHERE learner_id::text=$1`, owner).Scan(&optOut) == nil && !optOut && s.opts.Events != nil {
		props := map[string]any{
			"source":         "api",
			"platform":       "web",
			"app_version":    "0.1.0",
			"fields_changed": []string{"level"},
		}
		_ = s.opts.Events.Capture(owner, "learner_profile_updated", props)
	}

	respond(w, 200, map[string]any{
		"level_override": level,
	})
}
