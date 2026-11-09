package player

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Objects reads immutable versioned application keys; never a caller supplied URL.
type Objects interface {
	Get(context.Context, string) ([]byte, error)
}

// Options supplies storage and the trusted asset origin.
type Options struct {
	Pool        *pgxpool.Pool
	Objects     Objects
	AssetOrigin string
}

// Service serves enrolled lesson content without granting progress.
type Service struct{ opts Options }

// New creates a fail-closed player service.
func New(opts Options) *Service { return &Service{opts: opts} }
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		failure(w, 401)
		return
	}
	if s.opts.Pool == nil || s.opts.Objects == nil || !ValidOrigin(s.opts.AssetOrigin) {
		failure(w, 503)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	id := r.PathValue("lesson")
	var key, course, title string
	var version int
	err := s.opts.Pool.QueryRow(ctx, `SELECT m.manifest_key,m.version,c.id::text,l.title
 FROM lesson_manifests m JOIN course_lesson_stubs l ON l.id=m.lesson_id
 JOIN course_units u ON u.id=l.unit_id JOIN courses c ON c.id=u.course_id
 JOIN course_enrollments e ON e.course_id=c.id AND e.learner_id::text=$2
 WHERE m.lesson_id::text=$1 AND m.learner_id::text=$2 AND l.learner_id=m.learner_id AND c.deleted_at IS NULL`, id, owner).Scan(&key, &version, &course, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		failure(w, 404)
		return
	}
	if err != nil {
		failure(w, 503)
		return
	}
	if !validKey(key) {
		failure(w, 503)
		return
	}
	raw, err := s.opts.Objects.Get(ctx, key)
	if err != nil || len(raw) > maxObjectBytes {
		failure(w, 503)
		return
	}
	m, err := decodeManifest(raw, s.opts.AssetOrigin)
	if err != nil || m.Version != version {
		failure(w, 503)
		return
	}
	if r.PathValue("slide") == "" && strings.HasSuffix(r.URL.Path, "/position") {
		s.position(ctx, w, r, owner, id, m)
		return
	}
	if r.PathValue("slide") != "" {
		for _, slide := range m.Slides {
			if slide.ID != r.PathValue("slide") {
				continue
			}
			raw, assets, _, err := s.document(ctx, slide)
			if err != nil {
				failure(w, 503)
				return
			}
			respond(w, 200, map[string]any{"html": string(raw), "assets": assets})
			return
		}
		failure(w, 404)
		return
	}
	out := Manifest{LessonID: id, CourseID: course, Title: title, Version: version, AssetOrigin: s.opts.AssetOrigin, Slides: make([]Slide, 0, len(m.Slides))}
	for _, slide := range m.Slides {
		raw, _, hashes, err := s.document(ctx, slide)
		if err != nil {
			failure(w, 503)
			return
		}
		out.Slides = append(out.Slides, Slide{ID: slide.ID, DurationMS: slide.DurationMS, HTMLURL: "/v1/lessons/" + id + "/slides/" + slide.ID, Assets: slide.Assets, Checkpoints: slide.Checkpoints, NarrationStatus: "disabled", NarrationText: slide.NarrationText, HTMLSHA256: checksum(raw), AssetSHA256: hashes})
	}
	respond(w, 200, out)
}
func failure(w http.ResponseWriter, status int) {
	message := "lesson unavailable"
	if status == 401 {
		message = "sign in required"
	}
	respond(w, status, map[string]string{"error": message})
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
