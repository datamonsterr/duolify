package player

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// Position is a playback cursor, never lesson completion or assessed progress.
type Position struct {
	Version int    `json:"version"`
	SlideID string `json:"slide_id"`
	TimeMS  int    `json:"time_ms"`
}

func validPosition(p Position, m ObjectManifest) bool {
	if p.Version != m.Version || p.TimeMS < 0 {
		return false
	}
	for _, s := range m.Slides {
		if p.SlideID == s.ID {
			return p.TimeMS <= s.DurationMS
		}
	}
	return false
}
func decodePosition(reader io.Reader, m ObjectManifest) (Position, error) {
	var in struct {
		Version int    `json:"version"`
		SlideID string `json:"slide_id"`
		TimeMS  *int   `json:"time_ms"`
	}
	d := json.NewDecoder(reader)
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&in) != nil || d.Decode(&extra) != io.EOF || in.TimeMS == nil {
		return Position{}, errInvalid
	}
	p := Position{Version: in.Version, SlideID: in.SlideID, TimeMS: *in.TimeMS}
	if !validPosition(p, m) {
		return Position{}, errInvalid
	}
	return p, nil
}

func (s *Service) position(ctx context.Context, w http.ResponseWriter, r *http.Request, owner, lesson string, m ObjectManifest) {
	if r.Method == http.MethodGet {
		var p Position
		err := s.opts.Pool.QueryRow(ctx, `SELECT version,slide_id,time_ms FROM lesson_playback_positions WHERE learner_id::text=$1 AND lesson_id::text=$2`, owner, lesson).Scan(&p.Version, &p.SlideID, &p.TimeMS)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !validPosition(p, m)) {
			respond(w, 200, nil)
			return
		}
		if err != nil {
			failure(w, 503)
			return
		}
		respond(w, 200, p)
		return
	}
	p, err := decodePosition(http.MaxBytesReader(w, r.Body, 4096), m)
	if err != nil {
		respond(w, 400, map[string]string{"error": "invalid playback position"})
		return
	}
	// Recheck enrollment and the immutable version in the write statement too.
	result, err := s.opts.Pool.Exec(ctx, `INSERT INTO lesson_playback_positions(learner_id,lesson_id,version,slide_id,time_ms)
 SELECT m.learner_id,m.lesson_id,$3,$4,$5 FROM lesson_manifests m
 JOIN course_lesson_stubs l ON l.id=m.lesson_id JOIN course_units u ON u.id=l.unit_id
 JOIN courses c ON c.id=u.course_id JOIN course_enrollments e ON e.course_id=c.id AND e.learner_id=m.learner_id
 WHERE m.learner_id::text=$1 AND m.lesson_id::text=$2 AND m.version=$3 AND c.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM course_copies bc WHERE bc.copy_course_id=c.id AND bc.blocked_at IS NOT NULL)
 ON CONFLICT(learner_id,lesson_id) DO UPDATE SET version=EXCLUDED.version,slide_id=EXCLUDED.slide_id,time_ms=EXCLUDED.time_ms,updated_at=now()`, owner, lesson, p.Version, p.SlideID, p.TimeMS)
	if err != nil {
		failure(w, 503)
		return
	}
	if result.RowsAffected() != 1 {
		failure(w, 404)
		return
	}
	respond(w, 200, p)
}
