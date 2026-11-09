package player

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Objects reads immutable versioned application keys; never a caller supplied URL.
type Objects interface {
	Get(context.Context, string) ([]byte, error)
}

// Entitlements exposes funded subscription snapshot verification.
type Entitlements interface {
	FundedPlan(context.Context, string, time.Time) (*credits.FundedPlan, error)
}

// DownloadLessonItem projects one unlocked lesson available for course download.
type DownloadLessonItem struct {
	LessonID     string `json:"lesson_id"`
	UnitID       string `json:"unit_id"`
	Title        string `json:"title"`
	Position     int    `json:"position"`
	Status       string `json:"status"`
	Unlocked     bool   `json:"unlocked"`
	Downloadable bool   `json:"downloadable"`
	ManifestURL  string `json:"manifest_url"`
}

// CourseDownloadLibrary represents authorized course download metadata and unlocked lessons.
type CourseDownloadLibrary struct {
	CourseID string               `json:"course_id"`
	Title    string               `json:"title"`
	Entitled bool                 `json:"entitled"`
	PlanID   string               `json:"plan_id,omitempty"`
	Lessons  []DownloadLessonItem `json:"lessons"`
}

// IsPlanEntitledToDownload checks whether a funded plan entitles the learner to offline library downloads.
func IsPlanEntitledToDownload(plan *credits.FundedPlan) bool {
	if plan == nil || !plan.Active || plan.Revoked {
		return false
	}
	for _, b := range plan.Benefits {
		if strings.EqualFold(b, "Offline course library downloads") {
			return true
		}
	}
	return false
}

// ResolveDownloadItem constructs a DownloadLessonItem honestly deriving Unlocked and Downloadable.
func ResolveDownloadItem(lessonID, unitID, title string, position int, status string, hasManifest bool) DownloadLessonItem {
	unlocked := status != "locked"
	downloadable := unlocked && hasManifest
	manifestURL := ""
	if downloadable {
		manifestURL = "/v1/lessons/" + lessonID + "/manifest"
	}
	return DownloadLessonItem{
		LessonID:     lessonID,
		UnitID:       unitID,
		Title:        title,
		Position:     position,
		Status:       status,
		Unlocked:     unlocked,
		Downloadable: downloadable,
		ManifestURL:  manifestURL,
	}
}

// Options supplies storage and the trusted asset origin.
type Options struct {
	Pool        *pgxpool.Pool
	Objects     Objects
	AssetOrigin string
	Credits     Entitlements
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
	courseID := r.PathValue("course")
	if courseID == "" {
		courseID = r.PathValue("id")
	}
	if courseID != "" && strings.HasSuffix(r.URL.Path, "/download") {
		s.downloadLibrary(ctx, w, r, owner, courseID)
		return
	}
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
	if r.PathValue("slide") == "" && strings.HasSuffix(r.URL.Path, "/sync") {
		s.sync(ctx, w, r, owner, id, m)
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
		explanation := slide.Explanation
		if explanation == "" {
			explanation = slide.NarrationText
		}
		out.Slides = append(out.Slides, Slide{ID: slide.ID, DurationMS: slide.DurationMS, HTMLURL: "/v1/lessons/" + id + "/slides/" + slide.ID, Assets: slide.Assets, Checkpoints: slide.Checkpoints, NarrationStatus: "disabled", NarrationText: slide.NarrationText, Explanation: explanation, HTMLSHA256: checksum(raw), AssetSHA256: hashes})
	}
	respond(w, 200, out)
}
func (s *Service) downloadLibrary(ctx context.Context, w http.ResponseWriter, _ *http.Request, owner, courseID string) {
	if s.opts.Credits == nil {
		failure(w, 503)
		return
	}
	plan, err := s.opts.Credits.FundedPlan(ctx, owner, time.Now().UTC())
	if err != nil {
		failure(w, 503)
		return
	}

	if !IsPlanEntitledToDownload(plan) {
		respond(w, 403, map[string]string{
			"error":   "subscription_required",
			"message": "Subscription required to download offline library",
		})
		return
	}

	var courseTitle string
	err = s.opts.Pool.QueryRow(ctx, `SELECT c.title
		FROM courses c JOIN course_enrollments e ON e.course_id=c.id AND e.learner_id::text=$2
		WHERE c.id::text=$1 AND c.deleted_at IS NULL`, courseID, owner).Scan(&courseTitle)
	if errors.Is(err, pgx.ErrNoRows) {
		failure(w, 404)
		return
	}
	if err != nil {
		failure(w, 503)
		return
	}

	rows, err := s.opts.Pool.Query(ctx, `SELECT l.id::text, u.id::text, l.title, l.position, l.status,
		EXISTS(SELECT 1 FROM lesson_manifests m WHERE m.lesson_id=l.id AND m.learner_id::text=$2) AS has_manifest
		FROM course_lesson_stubs l
		JOIN course_units u ON u.id=l.unit_id
		JOIN courses c ON c.id=u.course_id
		WHERE c.id::text=$1 AND l.learner_id::text=$2
		ORDER BY u.position, l.position`, courseID, owner)
	if err != nil {
		failure(w, 503)
		return
	}
	defer rows.Close()

	items := []DownloadLessonItem{}
	for rows.Next() {
		var lessonID, unitID, title, status string
		var position int
		var hasManifest bool
		if err := rows.Scan(&lessonID, &unitID, &title, &position, &status, &hasManifest); err != nil {
			failure(w, 503)
			return
		}
		items = append(items, ResolveDownloadItem(lessonID, unitID, title, position, status, hasManifest))
	}

	planID := ""
	if plan != nil {
		planID = plan.PlanID
	}

	respond(w, 200, CourseDownloadLibrary{
		CourseID: courseID,
		Title:    courseTitle,
		Entitled: true,
		PlanID:   planID,
		Lessons:  items,
	})
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
