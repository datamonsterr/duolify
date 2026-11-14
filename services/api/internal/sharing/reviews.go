package sharing

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/social"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReviewScreener is the platform-funded ten-second classifier boundary.
type ReviewScreener interface {
	ScreenReview(context.Context, *pb.ScreenReviewRequest) (*pb.ScreenReviewResponse, error)
}

// ConfigureReviewScreening supports independent classifier doubles and deployments.
func (s *Service) ConfigureReviewScreening(r ReviewScreener) { s.reviewScreen = r }

var (
	// ErrInvalidReview rejects star and Unicode text bounds.
	ErrInvalidReview = errors.New("invalid_review")
	// ErrSelfReview refuses owner ratings.
	ErrSelfReview = errors.New("self_review")
)

// ReviewGateError carries a prerequisite count, never private Course information.
type ReviewGateError struct{ Needed int }

func (e *ReviewGateError) Error() string { return "review_locked" }

// Review exposes text only when it passed screening. No learner identifiers are public.
type Review struct {
	ID        string    `json:"id"`
	Stars     int       `json:"stars"`
	Text      string    `json:"text"`
	Version   int       `json:"version"`
	Verdict   string    `json:"verdict"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}

// ReviewsPage includes session-specific eligibility alongside a bounded public list.
type ReviewsPage struct {
	Items            []Review `json:"items"`
	Mine             *Review  `json:"mine"`
	LessonsNeeded    int      `json:"lessons_needed"`
	CompletedLessons int      `json:"completed_lessons"`
	MinLessons       int      `json:"min_lessons"`
	MaxChars         int      `json:"max_chars"`
	Owner            bool     `json:"owner"`
	CanReview        bool     `json:"can_review"`
	RatingCount      int      `json:"rating_count"`
	RatingAverage    *float64 `json:"rating_average"`
}

// ValidateReview counts Unicode characters, as Postgres char_length does.
func ValidateReview(stars int, text string, cfg Config) error {
	if stars < 1 || stars > 5 || !utf8.ValidString(text) || utf8.RuneCountInString(text) > cfg.ReviewMaxChars || strings.ContainsRune(text, 0) {
		return ErrInvalidReview
	}
	return nil
}

func (s *Service) reviewCourse(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, actor, course string) (string, error) {
	if !s.cfg.MarketplaceEnabled {
		return "", ErrMarketplaceDisabled
	}
	if !validID(course) || (actor != "" && !validID(actor)) {
		return "", ErrCourseUnavailable
	}
	var owner string
	err := q.QueryRow(ctx, `SELECT l.owner_id::text FROM marketplace_listings l JOIN courses c ON c.id=l.course_id
 WHERE l.course_id=$1 AND l.delisted_at IS NULL AND c.deleted_at IS NULL AND c.visibility='public'`+social.NotBlockedSQL("$2::uuid", "l.owner_id"), course, nullUUID(actor)).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrCourseUnavailable
	}
	return owner, err
}

func (s *Service) reviewProgress(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, actor, course string) (version, done int, err error) {
	err = q.QueryRow(ctx, `SELECT cc.version,(SELECT count(DISTINCT x.lesson_id) FROM lesson_completion_outbox x
 WHERE x.learner_id=cc.learner_id AND x.course_id=cc.copy_course_id AND x.state='passed')
 FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id WHERE cc.learner_id=$1 AND cc.source_course_id=$2 AND c.deleted_at IS NULL`, nullUUID(actor), course).Scan(&version, &done)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}

// Reviews never returns unscreened text, including the viewer's own pending review.
func (s *Service) Reviews(ctx context.Context, actor, course string) (ReviewsPage, error) {
	p := ReviewsPage{Items: []Review{}, MinLessons: s.cfg.ReviewMinLessons, MaxChars: s.cfg.ReviewMaxChars}
	owner, err := s.reviewCourse(ctx, s.pool, actor, course)
	if err != nil {
		return p, err
	}
	p.Owner = actor == owner
	_, p.CompletedLessons, err = s.reviewProgress(ctx, s.pool, actor, course)
	if err != nil {
		return p, err
	}
	p.LessonsNeeded = max(0, p.MinLessons-p.CompletedLessons)
	p.CanReview = actor != "" && !p.Owner && p.LessonsNeeded == 0
	var sum int64
	if err = s.pool.QueryRow(ctx, `SELECT rating_sum,rating_count FROM marketplace_listing_stats WHERE course_id=$1`, course).Scan(&sum, &p.RatingCount); err != nil {
		return p, err
	}
	if p.RatingCount > 0 {
		avg := float64(sum) / float64(p.RatingCount)
		p.RatingAverage = &avg
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id::text,r.stars,CASE WHEN r.verdict='pass' THEN r.text ELSE '' END,r.version,r.verdict,
 CASE WHEN r.learner_id IS NULL THEN 'deleted user' ELSE COALESCE(u.username,'Learner') END,r.created_at,COALESCE(r.learner_id::text,'')
 FROM course_reviews r LEFT JOIN learner_usernames u ON u.learner_id=r.learner_id
 WHERE r.course_id=$1`+social.NotBlockedSQL("$2::uuid", "r.learner_id")+` ORDER BY (r.learner_id=$2::uuid) DESC NULLS LAST,r.created_at DESC,r.id DESC LIMIT $3`, course, nullUUID(actor), s.cfg.MarketplaceMaxPageSize)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Review
		var learner string
		if err = rows.Scan(&r.ID, &r.Stars, &r.Text, &r.Version, &r.Verdict, &r.Author, &r.CreatedAt, &learner); err != nil {
			return p, err
		}
		if learner == actor && actor != "" {
			mine := r
			p.Mine = &mine
		}
		p.Items = append(p.Items, r)
	}
	return p, rows.Err()
}

// SaveReview serializes eligibility and daily caps, then screens without holding database locks.
// Its operation ID prevents stale screening results from restoring deleted or edited text.
func (s *Service) SaveReview(ctx context.Context, actor, course string, stars int, text string) (Review, error) {
	var r Review
	if !validID(actor) {
		return r, ErrCourseUnavailable
	}
	if err := ValidateReview(stars, text, s.cfg); err != nil {
		return r, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM learners WHERE id=$1 FOR NO KEY UPDATE`, actor).Scan(&locked); err != nil {
		return r, ErrCourseUnavailable
	}
	owner, err := s.reviewCourse(ctx, tx, actor, course)
	if err != nil {
		return r, err
	}
	if owner == actor {
		return r, ErrSelfReview
	}
	version, done, err := s.reviewProgress(ctx, tx, actor, course)
	if err != nil {
		return r, err
	}
	if done < s.cfg.ReviewMinLessons {
		return r, &ReviewGateError{Needed: s.cfg.ReviewMinLessons - done}
	}
	var n int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM course_review_submissions WHERE learner_id=$1 AND created_at>now()-interval '24 hours'`, actor).Scan(&n); err != nil {
		return r, err
	}
	if n >= s.cfg.ReviewMaxPerDay {
		return r, ErrRateLimited
	}
	if _, err = tx.Exec(ctx, `INSERT INTO course_review_submissions(learner_id) VALUES($1)`, actor); err != nil {
		return r, err
	}
	op := uuid.NewString()
	err = tx.QueryRow(ctx, `INSERT INTO course_reviews(course_id,learner_id,version,stars,text,operation_id)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(learner_id,course_id) DO UPDATE SET stars=EXCLUDED.stars,text=EXCLUDED.text,
 verdict='pending_screening',operation_id=EXCLUDED.operation_id,updated_at=now()
 RETURNING id::text,stars,version,created_at`, course, actor, version, stars, text, op).Scan(&r.ID, &r.Stars, &r.Version, &r.CreatedAt)
	if err != nil {
		return r, err
	}
	if err = tx.Commit(ctx); err != nil {
		return r, err
	}
	s.invalidateCatalogue(ctx)
	out := s.screenReview(ctx, actor, text)
	tx, err = s.pool.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE course_reviews SET verdict=$3 WHERE id=$1 AND operation_id=$2 AND learner_id=$4`, r.ID, op, out.Verdict, actor)
	if err != nil {
		return r, err
	}
	if tag.RowsAffected() == 0 {
		return r, ErrCourseUnavailable
	}
	if out.ReasonCodes == nil {
		out.ReasonCodes = []string{}
	}
	_, err = tx.Exec(ctx, `INSERT INTO course_review_screenings(review_id,learner_id,verdict,reason_codes,attempts,fallback_used,cost_nanos) VALUES($1,$2,$3,$4,$5,$6,$7)`, r.ID, actor, out.Verdict, out.ReasonCodes, out.Attempts, out.FallbackUsed, out.CostNanos)
	if err != nil {
		return r, err
	}
	if err = tx.Commit(ctx); err != nil {
		return r, err
	}
	r.Verdict = out.Verdict
	if out.Verdict == VerdictPass {
		r.Text = text
	}
	s.emit(actor, "review_submitted", map[string]any{"course_id": course, "review_id": r.ID, "rating": stars, "version": r.Version, "verdict": r.Verdict})
	return r, nil
}

func (s *Service) screenReview(ctx context.Context, actor, text string) Outcome {
	if strings.TrimSpace(text) == "" {
		return Outcome{Verdict: VerdictPass}
	}
	cfg := s.cfg
	cfg.MaxInputTokens = 4000
	cfg.MaxOutputTokens = 300
	cfg.TimeoutMs = min(cfg.ReviewTimeoutMs, 10000)
	cfg.MaxAttempts = 2
	cfg.MaxCostNanos = min(cfg.ReviewMaxCostNanos, 20_000_000)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
	defer cancel()
	var resp *pb.ScreenReviewResponse
	var err error
	var rates Rates
	if s.rates == nil || s.reviewScreen == nil {
		return Outcome{Verdict: VerdictPending}
	}
	rates, err = s.rates.Rates(ctx)
	if err != nil {
		return Outcome{Verdict: VerdictPending}
	}
	deadline, _ := ctx.Deadline()
	resp, err = s.reviewScreen.ScreenReview(ctx, &pb.ScreenReviewRequest{Text: text, Limits: &pb.UsageLimits{InputTokens: 4000, OutputTokens: 300, TimeoutMs: cfg.TimeoutMs, MaxAttempts: 2, MaxCostNanos: boundedUint64(cfg.MaxCostNanos), InputNanos: rates.Input, OutputNanos: rates.Output, CacheWriteNanos: rates.CacheWrite, CacheReadNanos: rates.CacheRead, DeadlineUnixMs: boundedUint64(deadline.UnixMilli())}})
	reported := false
	if resp != nil {
		for _, a := range resp.Attempts {
			if a == nil {
				continue
			}
			reported = true
			s.emit(actor, "$ai_generation", map[string]any{"feature": "screen_review", "provider": a.Provider, "model": a.Model, "input_tokens": a.InputTokens, "output_tokens": a.OutputTokens, "cache_write_tokens": a.CacheWriteTokens, "cache_read_tokens": a.CacheReadTokens, "attempt": a.Attempt, "latency_ms": a.LatencyMs, "succeeded": a.Delivered, "usage_available": !a.UsageUnavailable, "fallback_used": a.FallbackUsed, "error_code": a.ErrorCode, "provider_cost_nanos": attemptCost(a, rates)})
		}
	}
	if !reported {
		// A failed transport may have consumed provider work. Report unknown usage without
		// inventing a measured zero or copying a private provider error into analytics.
		s.emit(actor, "$ai_generation", map[string]any{"feature": "screen_review", "provider": "unavailable", "model": "unavailable", "attempt": 1, "succeeded": false, "usage_available": false, "fallback_used": false, "error_code": "screening_unavailable"})
	}
	return DecideReview(resp, err, cfg, rates)
}

// DecideReview validates the shared envelope plus review policy and sequential fallback usage.
func DecideReview(resp *pb.ScreenReviewResponse, callErr error, cfg Config, rates Rates) Outcome {
	cfg.MaxInputTokens = 4000
	cfg.MaxOutputTokens = 300
	cfg.TimeoutMs = min(cfg.ReviewTimeoutMs, 10000)
	cfg.MaxAttempts = 2
	cfg.MaxCostNanos = min(cfg.ReviewMaxCostNanos, 20_000_000)
	if resp == nil {
		return Outcome{Verdict: VerdictPending}
	}
	policy := ""
	if resp.PolicyVersion == "review-v1" {
		policy = PolicyVersion
	}
	compatible := &pb.ScreenCourseResponse{Verdict: resp.Verdict, ReasonCodes: resp.ReasonCodes, PolicyVersion: policy, Attempts: resp.Attempts}
	var total uint64
	for i, a := range resp.Attempts {
		if a == nil {
			continue
		}
		total += uint64(a.LatencyMs)
		if a.Attempt != uint32(i+1) || a.FallbackUsed != (i == 1) {
			callErr = ErrInvalidReview
		}
	}
	if total > uint64(cfg.TimeoutMs) {
		callErr = ErrInvalidReview
	}
	return Decide(nil, compatible, callErr, cfg, rates)
}

// DeleteReview deletes only the session actor's contribution; repeated deletes are harmless.
func (s *Service) DeleteReview(ctx context.Context, actor, course string) error {
	if !validID(actor) {
		return ErrCourseUnavailable
	}
	if _, err := s.reviewCourse(ctx, s.pool, actor, course); err != nil {
		return err
	}
	var id string
	err := s.pool.QueryRow(ctx, `DELETE FROM course_reviews WHERE learner_id=$1 AND course_id=$2 RETURNING id::text`, actor, course).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	s.invalidateCatalogue(ctx)
	s.emit(actor, "review_deleted", map[string]any{"course_id": course, "review_id": id})
	return nil
}
