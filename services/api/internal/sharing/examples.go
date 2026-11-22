package sharing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/examples"
	"github.com/datamonsterr/duolify/services/api/internal/httpcache"
	"github.com/jackc/pgx/v5"
)

// ErrExampleNotFound is returned for a slug that is not an authored example Course.
var ErrExampleNotFound = errors.New("example course not found")

// ExampleSource is the closed provenance recorded on a copy of an authored example.
const ExampleSource = "example"

// ExampleObjectPrefix is the immutable object prefix of one authored version. A version is never
// rewritten with different bytes, so every learner pinned to it shares these objects; they live
// outside lessons/published/ and lessons/generated/ so no purge job ever removes them.
func ExampleObjectPrefix(slug string, version int) string {
	return fmt.Sprintf("lessons/examples/%s/v%d", slug, version)
}

// ExampleSnapshot turns an authored Course into the snapshot shape a pinned copy is created from.
func ExampleSnapshot(c examples.Course) Snapshot {
	snap := Snapshot{Title: c.Title, Description: c.Description}
	for _, u := range c.Units {
		su := SnapshotUnit{Title: u.Title}
		for _, l := range u.Lessons {
			sl := SnapshotLesson{Key: c.Slug + "-" + l.Key, Title: l.Title, Goal: l.Goal, Generated: true}
			for _, s := range l.Slides {
				sl.Slides = append(sl.Slides, SnapshotSlide{ID: s.ID, HTML: s.HTML, Narration: s.Narration, DurationMS: s.DurationMS, TopicIDs: s.TopicIDs})
			}
			for _, q := range l.Test {
				sl.Questions = append(sl.Questions, SnapshotQ{Topic: q.TopicID, Prompt: q.Prompt, Choices: q.Choices, AnswerIndex: q.AnswerIndex, Explanation: q.Explanation})
			}
			for _, p := range l.VariantPool {
				variants := make([]examples.Question, len(p.Variants))
				for i, v := range p.Variants {
					v.TopicID = p.TopicID
					variants[i] = v
				}
				raw, _ := json.Marshal(variants)
				sl.RetryPool = append(sl.RetryPool, RetryTopic{Topic: p.TopicID, Variants: raw})
			}
			su.Lessons = append(su.Lessons, sl)
		}
		snap.Units = append(snap.Units, su)
	}
	return snap
}

// ExampleDocuments returns every immutable object of every authored version, byte for byte, for the
// versioned asset backup and for installing the objects.
func ExampleDocuments(origin string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, c := range examples.AllVersions() {
		snap := ExampleSnapshot(c)
		for ui, u := range snap.Units {
			for li, l := range u.Lessons {
				_, docs, err := LessonObjects(fmt.Sprintf("%s/%d-%d", ExampleObjectPrefix(c.Slug, c.Version), ui, li), l, 8000, origin)
				if err != nil {
					return nil, fmt.Errorf("%s v%d %s: %w", c.Slug, c.Version, l.Key, err)
				}
				for k, v := range docs {
					out[k] = v
				}
			}
		}
	}
	return out, nil
}

// InstallExampleObjects publishes every authored version's immutable objects. It is idempotent
// (identical bytes under identical keys), so two replicas starting together or a rerun are safe.
func (s *Service) InstallExampleObjects(ctx context.Context) error {
	if s.objects == nil {
		return nil
	}
	docs, err := ExampleDocuments(s.assetOrigin)
	if err != nil {
		return err
	}
	for key, raw := range docs {
		if err = s.objects.Put(ctx, key, raw); err != nil {
			return err
		}
	}
	return nil
}

// EnrollExample gives the learner exactly one free copy of an authored example, pinned to the
// version current now. A repeat returns the same copy (and keeps its pinned version, even after a
// newer version ships). Nothing here quotes, reserves or settles Credits or calls the engine.
func (s *Service) EnrollExample(ctx context.Context, viewer, slug string) (Enrollment, error) {
	course, ok := examples.Get(slug)
	if !ok {
		return Enrollment{}, ErrExampleNotFound
	}
	if !validID(viewer) {
		return Enrollment{}, ErrLinkNotFound
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Enrollment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "example-copy:"+viewer+":"+slug); err != nil {
		return Enrollment{}, err
	}
	var existing string
	var version int
	var deleted bool
	err = tx.QueryRow(ctx, `SELECT cc.copy_course_id::text, cc.version, c.deleted_at IS NOT NULL FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id
WHERE cc.learner_id=$1 AND cc.example_slug=$2`, viewer, slug).Scan(&existing, &version, &deleted)
	switch {
	case err == nil && !deleted:
		if err = activateCourse(ctx, tx, viewer, existing); err != nil {
			return Enrollment{}, err
		}
		total, ready, cerr := s.counts(ctx, tx, existing)
		if cerr != nil {
			return Enrollment{}, cerr
		}
		if err = tx.Commit(ctx); err != nil {
			return Enrollment{}, err
		}
		return Enrollment{CourseID: existing, Version: version, TotalLessons: total, ReadyLessons: ready}, nil
	case err == nil: // the learner removed their copy: enroll afresh
		if _, err = tx.Exec(ctx, `DELETE FROM courses WHERE id=$1`, existing); err != nil {
			return Enrollment{}, err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return Enrollment{}, err
	}
	snap := ExampleSnapshot(course)
	plan := PlanCopy(snap, s.assetOrigin)
	if plan.Ready != plan.Total || plan.Total == 0 {
		return Enrollment{}, fmt.Errorf("example %s v%d is not fully playable", slug, course.Version)
	}
	copyID, err := s.createExampleCopy(ctx, tx, viewer, course, snap, plan)
	if err != nil {
		return Enrollment{}, err
	}
	if err = activateCourse(ctx, tx, viewer, copyID); err != nil {
		return Enrollment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Enrollment{}, err
	}
	return Enrollment{CourseID: copyID, Created: true, Version: course.Version, TotalLessons: plan.Total, ReadyLessons: plan.Ready}, nil
}

func activateCourse(ctx context.Context, tx pgx.Tx, viewer, courseID string) error {
	if _, err := tx.Exec(ctx, `UPDATE course_enrollments SET last_active_at=now() WHERE learner_id=$1 AND course_id=$2`, viewer, courseID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE learner_profiles SET active_course_id=$2 WHERE learner_id=$1`, viewer, courseID)
	return err
}

// createExampleCopy mirrors createCopy for a system-authored version: no source course, owner or
// publication draft; the copy records the example slug, version and the hash of the authored bytes.
func (s *Service) createExampleCopy(ctx context.Context, tx pgx.Tx, viewer string, course examples.Course, snap Snapshot, plan CopyPlan) (string, error) {
	var copyID string
	if err := tx.QueryRow(ctx, `INSERT INTO courses(learner_id,title,description) VALUES($1,$2,$3) RETURNING id::text`, viewer, snap.Title, snap.Description).Scan(&copyID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1,$2)`, viewer, copyID); err != nil {
		return "", err
	}
	for ui, u := range plan.Units {
		var unitID string
		if err := tx.QueryRow(ctx, `INSERT INTO course_units(learner_id,course_id,authored_id,position,title) VALUES($1,$2,$3,$4,$5) RETURNING id::text`, viewer, copyID, fmt.Sprintf("u%d", ui+1), ui, u.Title).Scan(&unitID); err != nil {
			return "", err
		}
		for li, l := range u.Lessons {
			var stubID string
			if err := tx.QueryRow(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status,reused_from_snapshot,lesson_key) VALUES($1,$2,$3,$4,$5,$6,$7,true,$8) RETURNING id::text`,
				viewer, unitID, fmt.Sprintf("l%d", li+1), li, strings.TrimSpace(l.Title), strings.TrimSpace(l.Goal), l.Status, KeyOrLegacy(l.Source, ui, li)).Scan(&stubID); err != nil {
				return "", err
			}
			prefix := fmt.Sprintf("%s/%d-%d", ExampleObjectPrefix(course.Slug, course.Version), ui, li)
			if err := s.copyLesson(ctx, tx, viewer, copyID, stubID, "", prefix, course.Version, l.Source); err != nil {
				return "", err
			}
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO course_copies(copy_course_id,learner_id,source_course_id,source_owner_id,draft_id,version,content_hash,source,example_slug) VALUES($1,$2,NULL,NULL,NULL,$3,$4,$5,$6)`,
		copyID, viewer, course.Version, course.ContentHash(), ExampleSource, course.Slug); err != nil {
		return "", err
	}
	return copyID, nil
}

// ExamplesHandler serves the example catalogue and free enrollment.
type ExamplesHandler struct{ s *Service }

// NewExamplesHandler wires the example routes.
func NewExamplesHandler(s *Service) *ExamplesHandler { return &ExamplesHandler{s: s} }

// exampleSurfaces is the closed set of places that can offer an example.
var exampleSurfaces = map[string]bool{"courses": true, "learn_empty": true, "onboarding": true}

func (h *ExamplesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor := accounts.LearnerID(r.Context())
	if actor == "" {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method == http.MethodGet {
		// The same for every learner, but behind a session: private, revalidated, 304 on no change.
		httpcache.WriteJSON(w, r, http.StatusOK, httpcache.Private, map[string]any{"examples": examples.Catalogue()})
		return
	}
	var body struct {
		Surface string `json:"surface"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body)
	}
	got, err := h.s.EnrollExample(r.Context(), actor, r.PathValue("slug"))
	switch {
	case errors.Is(err, ErrExampleNotFound):
		write(w, http.StatusNotFound, map[string]string{"error": "example_not_found"})
	case err != nil:
		slog.ErrorContext(r.Context(), "example enroll failed", "error", err)
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "sharing_unavailable"})
	default:
		if got.Created {
			surface := body.Surface
			if !exampleSurfaces[surface] {
				surface = "courses"
			}
			h.s.emit(actor, "example_course_enrolled", map[string]any{"slug": r.PathValue("slug"), "surface": surface, "course_id": got.CourseID})
		}
		w.Header().Set("Cache-Control", "no-store")
		write(w, http.StatusOK, map[string]string{"course_id": got.CourseID})
	}
}
