package courses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// ServeCoursesListHTTP returns all confirmed courses enrolled by the authenticated learner.
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
	_ = s.opts.Pool.QueryRow(r.Context(), `SELECT COALESCE(active_course_id::text, '') FROM learner_profiles WHERE learner_id::text=$1`, owner).Scan(&activeCourseID)

	var coursesRaw []byte
	err := s.opts.Pool.QueryRow(r.Context(), `
		SELECT COALESCE(jsonb_agg(
			jsonb_build_object(
				'id', c.id,
				'title', c.title,
				'description', c.description,
				'level_override', COALESCE(e.level_override, ''),
				'units', COALESCE((
					SELECT jsonb_agg(
						jsonb_build_object(
							'id', u.id,
							'title', u.title,
							'lessons', COALESCE((
								SELECT jsonb_agg(
									jsonb_build_object(
										'id', l.id,
										'title', l.title,
										'goal', l.goal,
										'state', l.status
									) ORDER BY l.position
								) FROM course_lesson_stubs l WHERE l.unit_id=u.id
							), '[]'::jsonb)
						) ORDER BY u.position
					) FROM course_units u WHERE u.course_id=c.id
				), '[]'::jsonb)
			) ORDER BY e.last_active_at DESC, e.created_at DESC
		), '[]'::jsonb)
		FROM courses c
		JOIN course_enrollments e ON e.course_id=c.id
		WHERE e.learner_id::text=$1 AND c.deleted_at IS NULL
	`, owner).Scan(&coursesRaw)
	if err != nil {
		outlineError(w, 500)
		return
	}

	type courseItem struct {
		ID string `json:"id"`
	}
	var items []courseItem
	_ = json.Unmarshal(coursesRaw, &items)

	if len(items) > 0 {
		found := false
		for _, item := range items {
			if item.ID == activeCourseID {
				found = true
				break
			}
		}
		if !found {
			activeCourseID = items[0].ID
		}
	} else {
		activeCourseID = ""
	}

	var coursesJSON any
	_ = json.Unmarshal(coursesRaw, &coursesJSON)

	respond(w, 200, map[string]any{
		"courses":          coursesJSON,
		"active_course_id": activeCourseID,
	})
}

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
