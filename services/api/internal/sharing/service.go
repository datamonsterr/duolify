package sharing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Screener is the platform-funded classifier boundary (the engine's ScreenCourse RPC).
type Screener interface {
	ScreenCourse(context.Context, *pb.ScreenCourseRequest) (*pb.ScreenCourseResponse, error)
}

// RateSource supplies per-token USD nanos from the active Pricing config.
type RateSource interface {
	Rates(context.Context) (Rates, error)
}

// AgeSource reports whether a Learner meets the configured publisher age. Unknown means false.
type AgeSource interface {
	MeetsMinAge(ctx context.Context, learnerID string, minAge int) (bool, error)
}

// Service builds immutable drafts and screens them. It never changes visibility and never
// touches private learning tables except to read allowlisted Course content.
type Service struct {
	pool   *pgxpool.Pool
	events analytics.Events
	cfg    Config
	screen Screener
	rates  RateSource
	age    AgeSource
}

// NewService wires the service behind the central fail-closed analytics consent gate.
func NewService(pool *pgxpool.Pool, events analytics.Events, cfg Config) *Service {
	if events != nil {
		events = analytics.WithConsent(events, analytics.DatabaseConsent(pool))
	}
	return &Service{pool: pool, events: events, cfg: cfg}
}

// ConfigureScreening injects the platform-funded classifier and the Pricing-config rate source.
func (s *Service) ConfigureScreening(screen Screener, rates RateSource) {
	s.screen, s.rates = screen, rates
}

// WithAgeSource returns a copy so callers can vary the source without sharing state.
func (s *Service) WithAgeSource(a AgeSource) *Service { c := *s; c.age = a; return &c }

func validID(id string) bool { _, err := uuid.Parse(id); return err == nil }

// owns locks the Course row for its owner; anything else is the neutral ErrUnavailable.
func owns(ctx context.Context, q pgx.Tx, owner, courseID string) error {
	if !validID(owner) || !validID(courseID) {
		return ErrUnavailable
	}
	var id string
	err := q.QueryRow(ctx, `SELECT id::text FROM courses WHERE id=$1 AND learner_id=$2 AND deleted_at IS NULL FOR UPDATE`, courseID, owner).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnavailable
	}
	return err
}

// BuildDraft snapshots the Course, then screens it. A screening failure still returns the saved
// draft as pending_screening.
func (s *Service) BuildDraft(ctx context.Context, owner, courseID string) (Draft, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Draft{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = owns(ctx, tx, owner, courseID); err != nil {
		return Draft{}, err
	}
	snap, err := s.load(ctx, tx, courseID)
	if err != nil {
		return Draft{}, err
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return Draft{}, err
	}
	if err = CheckAllowlist(raw); err != nil {
		return Draft{}, err
	}
	sum := sha256.Sum256(raw)
	findings := ScanFindings(snap)
	lessons := 0
	for _, u := range snap.Units {
		lessons += len(u.Lessons)
	}
	if findings == nil {
		findings = []Finding{}
	}
	fj, _ := json.Marshal(findings)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO publication_drafts(course_id,owner_id,number,content_hash,snapshot,lesson_count,findings)
SELECT $1,$2,COALESCE(max(number),0)+1,$3,$4,$5,$6 FROM publication_drafts WHERE course_id=$1 RETURNING id::text`,
		courseID, owner, hex.EncodeToString(sum[:]), raw, lessons, fj).Scan(&id)
	if err != nil {
		return Draft{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Draft{}, err
	}
	s.runScreen(ctx, owner, id, snap, findings)
	return s.Get(ctx, owner, courseID, id)
}

func (s *Service) load(ctx context.Context, tx pgx.Tx, courseID string) (Snapshot, error) {
	var snap Snapshot
	if err := tx.QueryRow(ctx, `SELECT title,description FROM courses WHERE id=$1`, courseID).Scan(&snap.Title, &snap.Description); err != nil {
		return snap, err
	}
	rows, err := tx.Query(ctx, `SELECT u.position,u.title,st.id::text,COALESCE(g.title,st.title),st.goal
FROM course_units u JOIN course_lesson_stubs st ON st.unit_id=u.id LEFT JOIN generated_lessons g ON g.lesson_stub_id=st.id
WHERE u.course_id=$1 ORDER BY u.position,st.position`, courseID)
	if err != nil {
		return snap, err
	}
	type key struct{ unit, lesson int }
	index := map[string]key{}
	lastUnit := -1
	for rows.Next() {
		var pos int
		var ut, sid string
		var l SnapshotLesson
		if err = rows.Scan(&pos, &ut, &sid, &l.Title, &l.Goal); err != nil {
			rows.Close()
			return snap, err
		}
		if pos != lastUnit {
			snap.Units = append(snap.Units, SnapshotUnit{Title: ut})
			lastUnit = pos
		}
		u := &snap.Units[len(snap.Units)-1]
		u.Lessons = append(u.Lessons, l)
		index[sid] = key{len(snap.Units) - 1, len(u.Lessons) - 1}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return snap, err
	}
	at := func(sid string) *SnapshotLesson {
		k, ok := index[sid]
		if !ok {
			return nil
		}
		return &snap.Units[k.unit].Lessons[k.lesson]
	}
	// Content tables are read by an explicit column list: nothing learner-specific is selected.
	srows, err := tx.Query(ctx, `SELECT s.lesson_stub_id::text,s.slide_id,s.html,s.narration FROM lesson_slides s
JOIN course_lesson_stubs st ON st.id=s.lesson_stub_id JOIN course_units u ON u.id=st.unit_id WHERE u.course_id=$1 ORDER BY s.lesson_stub_id,s.position`, courseID)
	if err != nil {
		return snap, err
	}
	for srows.Next() {
		var sid string
		var sl SnapshotSlide
		if err = srows.Scan(&sid, &sl.ID, &sl.HTML, &sl.Narration); err != nil {
			srows.Close()
			return snap, err
		}
		if l := at(sid); l != nil {
			l.Generated = true
			l.Slides = append(l.Slides, sl)
			if s.cfg.IncludeNarration {
				l.NarrationRefs = append(l.NarrationRefs, sl.ID)
			}
		}
	}
	srows.Close()
	if err = srows.Err(); err != nil {
		return snap, err
	}
	qrows, err := tx.Query(ctx, `SELECT q.lesson_stub_id::text,q.topic_id,q.prompt,q.choices,q.answer_index,q.explanation FROM lesson_questions q
JOIN course_lesson_stubs st ON st.id=q.lesson_stub_id JOIN course_units u ON u.id=st.unit_id WHERE u.course_id=$1 AND q.variant_of IS NULL ORDER BY q.lesson_stub_id,q.position`, courseID)
	if err != nil {
		return snap, err
	}
	for qrows.Next() {
		var sid string
		var q SnapshotQ
		var choices []byte
		if err = qrows.Scan(&sid, &q.Topic, &q.Prompt, &choices, &q.AnswerIndex, &q.Explanation); err != nil {
			qrows.Close()
			return snap, err
		}
		if err = json.Unmarshal(choices, &q.Choices); err != nil {
			qrows.Close()
			return snap, err
		}
		if l := at(sid); l != nil {
			l.Questions = append(l.Questions, q)
		}
	}
	qrows.Close()
	if err = qrows.Err(); err != nil {
		return snap, err
	}
	prows, err := tx.Query(ctx, `SELECT p.lesson_stub_id::text,p.topic_id,p.variants FROM lesson_retry_pools p
JOIN course_lesson_stubs st ON st.id=p.lesson_stub_id JOIN course_units u ON u.id=st.unit_id WHERE u.course_id=$1 ORDER BY p.lesson_stub_id,p.topic_id`, courseID)
	if err != nil {
		return snap, err
	}
	for prows.Next() {
		var sid string
		var r RetryTopic
		if err = prows.Scan(&sid, &r.Topic, &r.Variants); err != nil {
			prows.Close()
			return snap, err
		}
		if l := at(sid); l != nil {
			l.RetryPool = append(l.RetryPool, r)
		}
	}
	prows.Close()
	return snap, prows.Err()
}

func (s *Service) runScreen(ctx context.Context, owner, draftID string, snap Snapshot, findings []Finding) {
	ctx = context.WithoutCancel(ctx)
	sample, n := BuildSample(snap, s.cfg)
	var resp *pb.ScreenCourseResponse
	var callErr error
	rates, rateErr := Rates{}, errors.New("screening not configured")
	if s.rates != nil {
		rates, rateErr = s.rates.Rates(ctx)
	}
	if s.screen != nil && rateErr == nil {
		callCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutMs)*time.Millisecond)
		resp, callErr = s.screen.ScreenCourse(callCtx, &pb.ScreenCourseRequest{SampleJson: sample, SampledLessons: boundedUint32(n), Limits: &pb.UsageLimits{
			InputTokens: s.cfg.MaxInputTokens, OutputTokens: s.cfg.MaxOutputTokens, TimeoutMs: s.cfg.TimeoutMs, MaxAttempts: s.cfg.MaxAttempts,
			MaxCostNanos: boundedUint64(s.cfg.MaxCostNanos), InputNanos: rates.Input, OutputNanos: rates.Output, CacheWriteNanos: rates.CacheWrite, CacheReadNanos: rates.CacheRead,
			DeadlineUnixMs: boundedUint64(time.Now().Add(time.Duration(s.cfg.TimeoutMs) * time.Millisecond).UnixMilli()),
		}})
		cancel()
	} else {
		callErr = rateErr
	}
	out := Decide(findings, resp, callErr, s.cfg, rates)
	sampled := 0
	if out.Verdict != VerdictPending {
		sampled = n
	}
	tx, err := s.pool.Begin(ctx)
	if err == nil {
		defer func() { _ = tx.Rollback(ctx) }()
		codes := out.ReasonCodes
		if codes == nil {
			codes = []string{}
		}
		if _, err = tx.Exec(ctx, `UPDATE publication_drafts SET verdict=$2,reason_codes=$3,sampled_lessons=$4,screened_at=now() WHERE id=$1`, draftID, out.Verdict, codes, sampled); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO publication_screenings(draft_id,owner_id,verdict,reason_codes,policy_version,attempts,fallback_used,cost_nanos) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
				draftID, owner, out.Verdict, codes, PolicyVersion, out.Attempts, out.FallbackUsed, out.CostNanos)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
	}
	if err != nil {
		slog.ErrorContext(ctx, "publication screening not recorded", "error", err)
		return
	}
	s.emit(owner, "course_screening_completed", map[string]any{"verdict": out.Verdict, "lessons_sampled": sampled, "finding_count": len(findings), "fallback_used": out.FallbackUsed})
	if resp != nil {
		for _, a := range resp.Attempts {
			if a == nil {
				continue
			}
			s.emit(owner, "$ai_generation", map[string]any{"feature": "course_screen", "provider": a.Provider, "model": a.Model, "input_tokens": a.InputTokens, "output_tokens": a.OutputTokens,
				"cache_write_tokens": a.CacheWriteTokens, "cache_read_tokens": a.CacheReadTokens, "attempt": a.Attempt, "latency_ms": a.LatencyMs, "succeeded": a.Delivered,
				"usage_available": !a.UsageUnavailable, "fallback_used": a.FallbackUsed, "error_code": a.ErrorCode, "provider_cost_nanos": attemptCost(a, rates)})
		}
	}
}

func (s *Service) emit(owner, event string, props map[string]any) {
	if s.events == nil {
		return
	}
	props["source"], props["platform"], props["app_version"] = "api", "web", "0.1.0"
	if err := s.events.Capture(owner, event, props); err != nil {
		slog.Error("sharing analytics delivery failed", "error", err)
	}
}

// Get returns an owner's draft; every other caller gets ErrUnavailable.
func (s *Service) Get(ctx context.Context, owner, courseID, draftID string) (Draft, error) {
	if !validID(owner) || !validID(courseID) || !validID(draftID) {
		return Draft{}, ErrUnavailable
	}
	var d Draft
	var snap, findings []byte
	err := s.pool.QueryRow(ctx, `SELECT d.id::text,d.course_id::text,d.number,d.content_hash,d.verdict,d.reason_codes,d.snapshot,d.findings,d.lesson_count,d.sampled_lessons,d.created_at
FROM publication_drafts d JOIN courses c ON c.id=d.course_id WHERE d.id=$1 AND d.course_id=$2 AND c.learner_id=$3 AND d.owner_id=$3 AND c.deleted_at IS NULL`, draftID, courseID, owner).
		Scan(&d.ID, &d.CourseID, &d.Number, &d.ContentHash, &d.Verdict, &d.ReasonCodes, &snap, &findings, &d.LessonCount, &d.SampledLessons, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Draft{}, ErrUnavailable
	}
	if err != nil {
		return Draft{}, err
	}
	var st Snapshot
	if err = json.Unmarshal(snap, &st); err != nil {
		return Draft{}, err
	}
	d.Outline = []OutlineUnit{}
	for _, u := range st.Units {
		ou := OutlineUnit{Title: u.Title, Lessons: []OutlineLesson{}}
		for _, l := range u.Lessons {
			ou.Lessons = append(ou.Lessons, OutlineLesson{Title: l.Title})
		}
		d.Outline = append(d.Outline, ou)
	}
	d.Findings = []Finding{}
	if err = json.Unmarshal(findings, &d.Findings); err != nil {
		return Draft{}, err
	}
	if d.Findings == nil { // stored null must still be a JSON array for clients
		d.Findings = []Finding{}
	}
	if d.ReasonCodes == nil {
		d.ReasonCodes = []string{}
	}
	d.NarrationIncl = s.cfg.IncludeNarration
	return d, nil
}

// Rescreen retries a draft that is still pending_screening; any other draft is returned as is.
func (s *Service) Rescreen(ctx context.Context, owner, courseID, draftID string) (Draft, error) {
	d, err := s.Get(ctx, owner, courseID, draftID)
	if err != nil || d.Verdict != VerdictPending {
		return d, err
	}
	var raw []byte
	if err = s.pool.QueryRow(ctx, `SELECT snapshot FROM publication_drafts WHERE id=$1`, draftID).Scan(&raw); err != nil {
		return Draft{}, err
	}
	var snap Snapshot
	if err = json.Unmarshal(raw, &snap); err != nil {
		return Draft{}, err
	}
	s.runScreen(ctx, owner, draftID, snap, d.Findings)
	return s.Get(ctx, owner, courseID, draftID)
}

// Publish is guarded now so E15-02 can add visibility behind the same checks: owner first,
// then the configured adult age, then no public visibility exists yet.
func (s *Service) Publish(ctx context.Context, owner, courseID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = owns(ctx, tx, owner, courseID); err != nil {
		return err
	}
	adult := false
	if s.age != nil {
		if adult, err = s.age.MeetsMinAge(ctx, owner, s.cfg.MinPublisherAge); err != nil {
			return err
		}
	}
	if !adult {
		return ErrAdultRequired
	}
	return ErrVisibilityUnavailable
}

// PurgeExpired deletes screening rows past retention (one year).
func (s *Service) PurgeExpired(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM publication_screenings WHERE created_at < now() - make_interval(days => $1)`, s.cfg.RetentionDays)
	return tag.RowsAffected(), err
}

// Maintain enforces the one-year screening retention even when no endpoint is visited.
func (s *Service) Maintain(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if _, err := s.PurgeExpired(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "publication screening purge failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
