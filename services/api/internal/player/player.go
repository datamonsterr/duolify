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
	// Narration serves the Learner's Retry and Restore actions for failed or missing audio (E14-02).
	Narration NarrationRecovery
}

// ErrNarrationNotFound hides slides that do not belong to the caller.
var ErrNarrationNotFound = errors.New("narration slide not found")

// NarrationRecovery repairs narration without charging the Learner.
type NarrationRecovery interface {
	Retry(ctx context.Context, owner, lesson, slide string) (string, error)
	Recover(ctx context.Context, owner, lesson, slide string) (string, error)
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
 WHERE m.lesson_id::text=$1 AND m.learner_id::text=$2 AND l.learner_id=m.learner_id AND l.status<>'locked' AND c.deleted_at IS NULL`, id, owner).Scan(&key, &version, &course, &title)
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
	if part := r.PathValue("part"); part != "" {
		if r.Method == http.MethodPost {
			s.narrationRecovery(ctx, w, owner, id, r.PathValue("slide"), part)
			return
		}
		s.narrationObject(ctx, w, owner, id, r.PathValue("slide"), part)
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
	cast, segments := s.pinnedCast(ctx, owner, id)
	narrations := s.narrations(ctx, owner, id)
	out := Manifest{Cast: cast, LessonID: id, CourseID: course, Title: title, Version: version, AssetOrigin: s.opts.AssetOrigin, Slides: make([]Slide, 0, len(m.Slides))}
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
		out.Slides = append(out.Slides, Slide{ID: slide.ID, DurationMS: slide.DurationMS, HTMLURL: "/v1/lessons/" + id + "/slides/" + slide.ID, Assets: slide.Assets, Checkpoints: slide.Checkpoints, NarrationStatus: narrations.status(slide.ID), NarrationReason: narrations[slide.ID].Reason, Narration: narrations[slide.ID].Asset, NarrationText: slide.NarrationText, Explanation: explanation, HTMLSHA256: checksum(raw), AssetSHA256: hashes, Segments: segments[slide.ID]})
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

// pinnedCast returns the job-pinned Cast and per-Slide dialogue stored with a
// generated Lesson. It never resolves current Characters, so edits and
// deletions affect only new jobs. Missing data yields a plain Lesson.
func (s *Service) pinnedCast(ctx context.Context, owner, lessonID string) (json.RawMessage, map[string]json.RawMessage) {
	var cast []byte
	err := s.opts.Pool.QueryRow(ctx, `SELECT c.snapshot FROM generated_lessons g JOIN lesson_cast_snapshots c ON c.course_id=g.course_id AND c.learner_id=g.learner_id AND c.lesson_key=g.manifest->>'cast_key' WHERE g.lesson_stub_id::text=$1 AND g.learner_id::text=$2`, lessonID, owner).Scan(&cast)
	if err != nil {
		return nil, nil
	}
	rows, err := s.opts.Pool.Query(ctx, `SELECT slide_id,segments FROM lesson_slides WHERE lesson_stub_id::text=$1 AND learner_id::text=$2`, lessonID, owner)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	segments := map[string]json.RawMessage{}
	for rows.Next() {
		var id string
		var raw []byte
		if rows.Scan(&id, &raw) == nil && string(raw) != "[]" {
			segments[id] = raw
		}
	}
	return cast, segments
}

type narrationState struct {
	Status string
	Reason string
	Asset  *NarrationAsset
}

type narrationStates map[string]narrationState

func (n narrationStates) status(slide string) string {
	if st := n[slide].Status; st != "" {
		return st
	}
	return "disabled"
}

// narrations projects per-slide narration status; a slide without a row is disabled.
func (s *Service) narrations(ctx context.Context, owner, lesson string) narrationStates {
	out := narrationStates{}
	// One row per slide: a stored (ready) voice wins over a pending one, and the Learner's chosen
	// voice wins among stored voices, so an old voice keeps playing until the new one is ready.
	rows, err := s.opts.Pool.Query(ctx, `SELECT DISTINCT ON (slide_id) slide_id,status,duration_ms,characters,failure_code FROM slide_narrations
		WHERE lesson_id::text=$1 AND learner_id::text=$2
		ORDER BY slide_id,(status='ready') DESC,(voice_key=COALESCE((SELECT narration_voice FROM learner_profiles WHERE learner_id::text=$2),'default')) DESC,updated_at DESC`, lesson, owner)
	if err != nil {
		return out // text and animation stay usable without audio
	}
	defer rows.Close()
	for rows.Next() {
		var slide, status, reason string
		var duration, characters int
		if rows.Scan(&slide, &status, &duration, &characters, &reason) != nil {
			continue
		}
		state := narrationState{Status: status}
		if status == "disabled" {
			state.Reason = reason
		}
		if status == "ready" {
			base := "/v1/lessons/" + lesson + "/slides/" + slide + "/narration/"
			state.Asset = &NarrationAsset{AudioURL: base + "audio", AlignmentURL: base + "alignment", DurationMS: duration, Characters: characters}
		}
		out[slide] = state
	}
	return out
}

// narrationObject serves owner-only audio or alignment bytes for a ready slide.
func (s *Service) narrationObject(ctx context.Context, w http.ResponseWriter, owner, lesson, slide, part string) {
	column, contentType := "audio_key", "audio/wav"
	if part == "alignment" {
		column, contentType = "alignment_key", "application/json"
	} else if part != "audio" {
		failure(w, 404)
		return
	}
	var key string
	err := s.opts.Pool.QueryRow(ctx, `SELECT `+column+` FROM slide_narrations WHERE lesson_id::text=$1 AND learner_id::text=$2 AND slide_id=$3 AND status='ready'
		ORDER BY (voice_key=COALESCE((SELECT narration_voice FROM learner_profiles WHERE learner_id::text=$2),'default')) DESC,updated_at DESC LIMIT 1`, lesson, owner, slide).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		failure(w, 404)
		return
	}
	if err != nil || !validKey(key) {
		failure(w, 503)
		return
	}
	raw, err := s.opts.Objects.Get(ctx, key)
	if err != nil || len(raw) > maxObjectBytes {
		failure(w, 503)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}

// narrationRecovery runs the Learner's Retry narration or Restore narration action. Neither
// reserves or charges Credits: retries reuse the content hash and restores reuse stored versions.
func (s *Service) narrationRecovery(ctx context.Context, w http.ResponseWriter, owner, lesson, slide, action string) {
	if s.opts.Narration == nil || (action != "retry" && action != "recover") {
		failure(w, 404)
		return
	}
	var state string
	var err error
	if action == "retry" {
		state, err = s.opts.Narration.Retry(ctx, owner, lesson, slide)
	} else {
		state, err = s.opts.Narration.Recover(ctx, owner, lesson, slide)
	}
	if errors.Is(err, ErrNarrationNotFound) {
		failure(w, 404)
		return
	}
	if err != nil {
		failure(w, 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_ = json.NewEncoder(w).Encode(map[string]string{"state": state})
}
