package sharing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/social"
	"github.com/jackc/pgx/v5"
)

// ObjectWriter stores immutable lesson objects (the same key space the player reads).
type ObjectWriter interface {
	Put(ctx context.Context, key string, raw []byte) error
}

// WithObjects returns a copy that can publish reused lesson manifests for the player.
func (s *Service) WithObjects(w ObjectWriter, assetOrigin string) *Service {
	c := *s
	c.objects, c.assetOrigin = w, assetOrigin
	return &c
}

// Enrollment is the result of enrolling: ids and counts only, never content or the token.
type Enrollment struct {
	CourseID               string `json:"course_id"`
	Created                bool   `json:"created"`
	Version                int    `json:"version"`
	TotalLessons           int    `json:"total_lessons"`
	ReadyLessons           int    `json:"ready_lessons"`
	NeedsGenerationLessons int    `json:"needs_generation_lessons"`
}

// EnrollmentState says whether the signed-in viewer already holds a copy for a live link.
type EnrollmentState struct {
	Enrolled bool   `json:"enrolled"`
	CourseID string `json:"course_id,omitempty"`
}

// LessonPlan is one Lesson of a copy: ready lessons are reused for free, others are left for
// generation at the learner's expense (E15-04).
type LessonPlan struct {
	Title, Goal string
	Ready       bool
	Status      string
	Source      SnapshotLesson
}

// UnitPlan is one unit of a copy.
type UnitPlan struct {
	Title   string
	Lessons []LessonPlan
}

// CopyPlan is the pure description of the copy to create.
type CopyPlan struct {
	Units        []UnitPlan
	Total, Ready int
}

// NeedsGeneration counts Lessons the pinned version does not contain.
func (p CopyPlan) NeedsGeneration() int { return p.Total - p.Ready }

// Reusable reports whether a snapshot Lesson is complete and safe to serve as is.
func Reusable(l SnapshotLesson, origin string) bool {
	if !l.Generated || len(l.Slides) == 0 || len(l.Questions) == 0 {
		return false
	}
	for _, q := range l.Questions {
		if strings.TrimSpace(q.Prompt) == "" || strings.TrimSpace(q.Explanation) == "" || len(q.Choices) < 2 || q.AnswerIndex < 0 || q.AnswerIndex >= len(q.Choices) {
			return false
		}
	}
	_, _, err := LessonObjects("lessons/published/check", l, 1000, origin)
	return err == nil
}

// PlanCopy marks each Lesson ready or needing generation and sets the gate: only the first Lesson
// is open, and it is playable at once only when the snapshot contains it.
func PlanCopy(snap Snapshot, origin string) CopyPlan {
	var plan CopyPlan
	for _, u := range snap.Units {
		up := UnitPlan{Title: u.Title}
		for _, l := range u.Lessons {
			lp := LessonPlan{Title: l.Title, Goal: l.Goal, Ready: Reusable(l, origin), Status: "locked", Source: l}
			if plan.Total == 0 {
				lp.Status = "ready_to_generate"
				if lp.Ready {
					lp.Status = "current"
				}
			}
			plan.Total++
			if lp.Ready {
				plan.Ready++
			}
			up.Lessons = append(up.Lessons, lp)
		}
		plan.Units = append(plan.Units, up)
	}
	return plan
}

// LessonObjects builds the immutable player objects for one Lesson: slides and a manifest,
// never questions, answers, explanations or retry pools. Keys derive from prefix, so identical
// input writes identical bytes and copies of one published version share them.
func LessonObjects(prefix string, l SnapshotLesson, slideMs int, origin string) (string, map[string][]byte, error) {
	m := player.ObjectManifest{Version: 1, Slides: []player.ObjectSlide{}}
	docs := map[string][]byte{}
	for i, sl := range l.Slides {
		if err := player.ValidateHTML(sl.HTML, origin); err != nil {
			return "", nil, err
		}
		key := fmt.Sprintf("%s/slide-%d.html", prefix, i)
		docs[key] = []byte(sl.HTML)
		m.Slides = append(m.Slides, player.ObjectSlide{ID: sl.ID, DurationMS: slideMs, HTMLKey: key, HTMLSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(sl.HTML))),
			Assets: []string{}, Checkpoints: []player.Checkpoint{}, NarrationText: sl.Narration})
	}
	if err := player.ValidateManifest(m, origin); err != nil {
		return "", nil, err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "", nil, err
	}
	key := prefix + "/manifest.json"
	docs[key] = raw
	return key, docs, nil
}

// resolveLink finds the live link's course and owner; every failure is the same neutral error.
func (s *Service) resolveLink(ctx context.Context, viewer, token string) (courseID, ownerID string, err error) {
	if token == "" || !validID(viewer) {
		return "", "", ErrLinkNotFound
	}
	err = s.pool.QueryRow(ctx, `SELECT pl.course_id::text, c.learner_id::text FROM publication_links pl JOIN courses c ON c.id=pl.course_id
WHERE pl.token_hash=$1 AND pl.revoked_at IS NULL AND c.deleted_at IS NULL AND c.visibility<>'private'`, HashToken(token)).Scan(&courseID, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrLinkNotFound
	}
	if err != nil {
		return "", "", err
	}
	if blocked, berr := social.BlockedEitherWay(ctx, s.pool, viewer, ownerID); berr != nil || blocked {
		return "", "", ErrLinkNotFound
	}
	return courseID, ownerID, nil
}

// Existing reports the viewer's copy for a live link without creating anything.
func (s *Service) Existing(ctx context.Context, viewer, token string) (EnrollmentState, error) {
	courseID, ownerID, err := s.resolveLink(ctx, viewer, token)
	if err != nil {
		return EnrollmentState{}, err
	}
	if viewer == ownerID {
		return EnrollmentState{Enrolled: true, CourseID: courseID}, nil
	}
	var id string
	err = s.pool.QueryRow(ctx, `SELECT cc.copy_course_id::text FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id
WHERE cc.learner_id=$1 AND cc.source_course_id=$2 AND c.deleted_at IS NULL`, viewer, courseID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnrollmentState{}, nil
	}
	return EnrollmentState{Enrolled: err == nil, CourseID: id}, err
}

func (s *Service) counts(ctx context.Context, tx pgx.Tx, courseID string) (total, ready int, err error) {
	err = tx.QueryRow(ctx, `SELECT count(*), count(g.id) FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id
LEFT JOIN generated_lessons g ON g.lesson_stub_id=l.id WHERE u.course_id=$1`, courseID).Scan(&total, &ready)
	return
}

// Enroll gives the viewer exactly one learner-owned copy of the link's course, pinned to the
// latest passing version. Repeats return the same copy. Reused lessons cost zero credits: nothing
// here quotes, reserves or settles.
func (s *Service) Enroll(ctx context.Context, viewer, token string) (Enrollment, error) {
	courseID, ownerID, err := s.resolveLink(ctx, viewer, token)
	if err != nil {
		return Enrollment{}, err
	}
	return s.enrollCourse(ctx, viewer, courseID, ownerID, "link")
}

// enrollCourse is the one free-enrollment path, shared by share links and the Marketplace; source
// is the closed provenance ("link" or "marketplace") recorded on the copy and in the event.
func (s *Service) enrollCourse(ctx context.Context, viewer, courseID, ownerID, source string) (Enrollment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Enrollment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if viewer == ownerID {
		total, ready, cerr := s.counts(ctx, tx, courseID)
		return Enrollment{CourseID: courseID, TotalLessons: total, ReadyLessons: ready, NeedsGenerationLessons: total - ready}, cerr
	}
	// One enroll per learner and source at a time; the unique index is the final guard.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "course-copy:"+viewer+":"+courseID); err != nil {
		return Enrollment{}, err
	}
	var existing string
	var version int
	var deleted bool
	err = tx.QueryRow(ctx, `SELECT cc.copy_course_id::text, cc.version, c.deleted_at IS NOT NULL FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id
WHERE cc.learner_id=$1 AND cc.source_course_id=$2`, viewer, courseID).Scan(&existing, &version, &deleted)
	switch {
	case err == nil && !deleted:
		total, ready, cerr := s.counts(ctx, tx, existing)
		return Enrollment{CourseID: existing, Version: version, TotalLessons: total, ReadyLessons: ready, NeedsGenerationLessons: total - ready}, cerr
	case err == nil: // the learner removed their copy: enroll afresh
		if _, err = tx.Exec(ctx, `DELETE FROM courses WHERE id=$1`, existing); err != nil {
			return Enrollment{}, err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return Enrollment{}, err
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM course_copies WHERE learner_id=$1 AND created_at > now() - interval '24 hours'`, viewer).Scan(&recent); err != nil {
		return Enrollment{}, err
	}
	if s.cfg.MaxEnrollmentsPerDay > 0 && recent >= s.cfg.MaxEnrollmentsPerDay {
		slog.WarnContext(ctx, "sharing abuse signal: daily enrollment limit reached", "learner_id", viewer, "limit", s.cfg.MaxEnrollmentsPerDay)
		return Enrollment{}, ErrRateLimited
	}
	var draftID, hash string
	var number int
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT id::text, number, content_hash, snapshot FROM publication_drafts WHERE course_id=$1 AND verdict='pass' ORDER BY number DESC LIMIT 1`, courseID).Scan(&draftID, &number, &hash, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Enrollment{}, ErrLinkNotFound
	}
	if err != nil {
		return Enrollment{}, err
	}
	var snap Snapshot
	if err = json.Unmarshal(raw, &snap); err != nil {
		return Enrollment{}, err
	}
	plan := PlanCopy(snap, s.assetOrigin)
	copyID, firstReady, err := s.createCopy(ctx, tx, viewer, courseID, draftID, hash, source, number, snap, plan)
	if err != nil {
		return Enrollment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Enrollment{}, err
	}
	s.refreshListingStats(ctx, courseID)
	s.emit(viewer, "course_enrolled_published", map[string]any{"source": source, "course_id": copyID, "source_course_id": courseID, "version": number})
	if firstReady != "" {
		s.emit(viewer, "published_lesson_reused", map[string]any{"course_id": copyID, "lesson_id": firstReady, "version": number})
	}
	return Enrollment{CourseID: copyID, Created: true, Version: number, TotalLessons: plan.Total, ReadyLessons: plan.Ready, NeedsGenerationLessons: plan.NeedsGeneration()}, nil
}

// createCopy writes the learner's own course, units, stubs and the content of every ready Lesson.
// It returns the open Lesson's id when that Lesson is playable at once (for the reuse event).
func (s *Service) createCopy(ctx context.Context, tx pgx.Tx, viewer, sourceID, draftID, hash, source string, number int, snap Snapshot, plan CopyPlan) (string, string, error) {
	var copyID, firstReady string
	if err := tx.QueryRow(ctx, `INSERT INTO courses(learner_id,title,description) VALUES($1,$2,$3) RETURNING id::text`, viewer, snap.Title, snap.Description).Scan(&copyID); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1,$2)`, viewer, copyID); err != nil {
		return "", "", err
	}
	for ui, u := range plan.Units {
		var unitID string
		if err := tx.QueryRow(ctx, `INSERT INTO course_units(learner_id,course_id,authored_id,position,title) VALUES($1,$2,$3,$4,$5) RETURNING id::text`, viewer, copyID, fmt.Sprintf("u%d", ui+1), ui, u.Title).Scan(&unitID); err != nil {
			return "", "", err
		}
		for li, l := range u.Lessons {
			var stubID string
			if err := tx.QueryRow(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status,reused_from_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`,
				viewer, unitID, fmt.Sprintf("l%d", li+1), li, strings.TrimSpace(l.Title), strings.TrimSpace(l.Goal), l.Status, l.Ready).Scan(&stubID); err != nil {
				return "", "", err
			}
			if !l.Ready {
				continue
			}
			if l.Status == "current" {
				firstReady = stubID
			}
			if err := s.copyLesson(ctx, tx, viewer, copyID, stubID, fmt.Sprintf("lessons/published/%s/%d-%d", draftID, ui, li), number, l.Source); err != nil {
				return "", "", err
			}
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO course_copies(copy_course_id,learner_id,source_course_id,draft_id,version,content_hash,source) VALUES($1,$2,$3,$4,$5,$6,$7)`, copyID, viewer, sourceID, draftID, number, hash, source); err != nil {
		return "", "", err
	}
	return copyID, firstReady, nil
}

func (s *Service) copyLesson(ctx context.Context, tx pgx.Tx, viewer, copyID, stubID, prefix string, version int, l SnapshotLesson) error {
	manifest, _ := json.Marshal(map[string]any{"slide_count": len(l.Slides), "question_count": len(l.Questions), "source": "published", "version": version})
	if _, err := tx.Exec(ctx, `INSERT INTO generated_lessons(lesson_stub_id,course_id,learner_id,title,manifest) VALUES($1,$2,$3,$4,$5)`, stubID, copyID, viewer, strings.TrimSpace(l.Title), manifest); err != nil {
		return err
	}
	for i, sl := range l.Slides {
		if _, err := tx.Exec(ctx, `INSERT INTO lesson_slides(lesson_stub_id,learner_id,position,slide_id,html,duration_ms,narration) VALUES($1,$2,$3,$4,$5,$6,$7)`, stubID, viewer, i, sl.ID, sl.HTML, s.cfg.CopySlideMs, sl.Narration); err != nil {
			return err
		}
	}
	for i, q := range l.Questions {
		choices, _ := json.Marshal(q.Choices)
		if _, err := tx.Exec(ctx, `INSERT INTO lesson_questions(lesson_stub_id,learner_id,topic_id,position,prompt,choices,answer_index,explanation) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, stubID, viewer, q.Topic, i, q.Prompt, choices, q.AnswerIndex, q.Explanation); err != nil {
			return err
		}
	}
	for _, r := range l.RetryPool {
		if _, err := tx.Exec(ctx, `INSERT INTO lesson_retry_pools(lesson_stub_id,learner_id,topic_id,variants) VALUES($1,$2,$3,$4) ON CONFLICT (lesson_stub_id,topic_id) DO NOTHING`, stubID, viewer, r.Topic, []byte(r.Variants)); err != nil {
			return err
		}
	}
	if s.objects == nil {
		return nil
	}
	key, docs, err := LessonObjects(prefix, l, s.cfg.CopySlideMs, s.assetOrigin)
	if err != nil {
		return err
	}
	for k, raw := range docs {
		if err = s.objects.Put(ctx, k, raw); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO lesson_manifests(lesson_id,learner_id,version,manifest_key) VALUES($1,$2,1,$3)`, stubID, viewer, key)
	return err
}
