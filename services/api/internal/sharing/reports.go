package sharing

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/datamonsterr/duolify/services/api/internal/social"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrInvalidReport rejects unknown reasons, targets, malformed ids and over-long or invalid text.
	ErrInvalidReport = errors.New("invalid_report")
	// ErrSelfReport refuses reporting yourself or your own content.
	ErrSelfReport = errors.New("self_report")

	usernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]{2,19}$`)
)

// ReportInput names the target. A user target is a public username, so no learner id is exposed.
type ReportInput struct{ TargetType, TargetID, Reason, Text string }

// ReportResult tells only the reporter whether the report merged into an earlier open one.
type ReportResult struct {
	Status string `json:"status"`
	Merged bool   `json:"merged"`
}

// QueueItem is the moderation read model: severity first, then age (E15-10 renders it).
type QueueItem struct {
	ID          string
	TargetType  string
	TargetID    string
	Severity    int
	ReportCount int
	OpenedAt    time.Time
}

// ValidateReport checks the closed vocabularies and counts Unicode characters like Postgres does.
func ValidateReport(in ReportInput, cfg Config) error {
	if !slices.Contains(cfg.ReportReasons, in.Reason) || !utf8.ValidString(in.Text) || strings.ContainsRune(in.Text, 0) || utf8.RuneCountInString(in.Text) > cfg.ReportMaxChars {
		return ErrInvalidReport
	}
	switch in.TargetType {
	case "course", "review":
		if !validID(in.TargetID) || len(in.TargetID) != 36 || in.TargetID != strings.ToLower(in.TargetID) {
			return ErrInvalidReport
		}
	case "user":
		if !usernamePattern.MatchString(in.TargetID) {
			return ErrInvalidReport
		}
	default:
		return ErrInvalidReport
	}
	return nil
}

// QueueSeverity is the highest configured severity among the reasons; unknown reasons count 0.
func QueueSeverity(reasons []string, cfg Config) int {
	top := 0
	for _, r := range reasons {
		top = max(top, cfg.ReportSeverity[r])
	}
	return top
}

// ShouldHide reports whether distinct reporters reached the configured threshold.
func ShouldHide(distinct int, cfg Config) bool { return distinct >= cfg.ReportHideThreshold }

type reportTarget struct {
	id, course, owner string
}

// resolveReportTarget finds the target and its owner. Anything unknown, deleted, listed out or blocked
// is the neutral ErrCourseUnavailable. A course target locks its listing row, which serializes the
// distinct-reporter count and so makes the hide decision race-safe.
func (s *Service) resolveReportTarget(ctx context.Context, tx pgx.Tx, actor string, in ReportInput) (reportTarget, error) {
	var t reportTarget
	var err error
	switch in.TargetType {
	case "course":
		// A hidden listing still accepts reports; the course stays hidden and the queue row is refreshed.
		err = tx.QueryRow(ctx, `SELECT l.course_id::text,l.course_id::text,l.owner_id::text FROM marketplace_listings l JOIN courses c ON c.id=l.course_id
 WHERE l.course_id=$1 AND l.delisted_at IS NULL AND c.deleted_at IS NULL AND c.visibility='public'`+social.NotBlockedSQL("$2::uuid", "l.owner_id")+` FOR UPDATE OF l`, in.TargetID, actor).Scan(&t.id, &t.course, &t.owner)
	case "review":
		err = tx.QueryRow(ctx, `SELECT r.id::text,r.course_id::text,r.learner_id::text FROM course_reviews r
 JOIN marketplace_listings l ON l.course_id=r.course_id JOIN courses c ON c.id=r.course_id
 WHERE r.id=$1 AND r.learner_id IS NOT NULL AND l.delisted_at IS NULL AND c.deleted_at IS NULL AND c.visibility='public'`+social.NotBlockedSQL("$2::uuid", "r.learner_id")+social.NotBlockedSQL("$2::uuid", "l.owner_id"), in.TargetID, actor).Scan(&t.id, &t.course, &t.owner)
	case "user":
		err = tx.QueryRow(ctx, `SELECT u.learner_id::text,'',u.learner_id::text FROM learner_usernames u
 WHERE u.canonical=lower($1)`+social.NotBlockedSQL("$2::uuid", "u.learner_id"), in.TargetID, actor).Scan(&t.id, &t.course, &t.owner)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrCourseUnavailable
	}
	if err == nil && t.owner == actor {
		err = ErrSelfReport
	}
	return t, err
}

// Report stores a private report. The reporter is only ever a database column, never part of any
// response, event or owner-visible payload. One open report per reporter and target (a repeat merges),
// a rolling daily cap, and at the configured number of DISTINCT reporters a course is hidden, its
// cache invalidated and one moderation queue row opened. Existing copies are never touched.
func (s *Service) Report(ctx context.Context, actor string, in ReportInput) (ReportResult, error) {
	res := ReportResult{Status: "received"}
	if !s.cfg.MarketplaceEnabled {
		return res, ErrMarketplaceDisabled
	}
	if !validID(actor) {
		return res, ErrCourseUnavailable
	}
	in.Text = strings.TrimSpace(in.Text)
	if err := ValidateReport(in, s.cfg); err != nil {
		return res, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Lock order: reporter, then listing. The reporter lock serializes their own cap and duplicates.
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM learners WHERE id=$1 FOR NO KEY UPDATE`, actor).Scan(&locked); err != nil {
		return res, ErrCourseUnavailable
	}
	target, err := s.resolveReportTarget(ctx, tx, actor, in)
	if err != nil {
		return res, err
	}
	var n int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM content_report_submissions WHERE reporter_id=$1 AND created_at>now()-interval '24 hours'`, actor).Scan(&n); err != nil {
		return res, err
	}
	if n >= s.cfg.ReportMaxPerDay {
		return res, ErrRateLimited
	}
	if _, err = tx.Exec(ctx, `INSERT INTO content_report_submissions(reporter_id) VALUES($1)`, actor); err != nil {
		return res, err
	}
	var reportID string
	if err = tx.QueryRow(ctx, `INSERT INTO content_reports(target_type,target_id,course_id,reporter_id,reason,text) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6)
 ON CONFLICT (reporter_id,target_type,target_id) WHERE status='open' DO UPDATE SET merge_count=content_reports.merge_count+1,
   text=CASE WHEN content_reports.text='' THEN EXCLUDED.text ELSE content_reports.text END,updated_at=now()
 RETURNING id::text,xmax<>0`, in.TargetType, target.id, target.course, actor, in.Reason, in.Text).Scan(&reportID, &res.Merged); err != nil {
		return res, err
	}
	var distinct int
	newlyHidden := false
	if in.TargetType == "course" {
		var reasons []string
		if err = tx.QueryRow(ctx, `SELECT count(DISTINCT reporter_id),COALESCE(array_agg(reason),'{}') FROM content_reports
 WHERE target_type='course' AND target_id=$1 AND status='open' AND reporter_id IS NOT NULL`, target.id).Scan(&distinct, &reasons); err != nil {
			return res, err
		}
		if ShouldHide(distinct, s.cfg) {
			var hidden bool
			if err = tx.QueryRow(ctx, `WITH u AS (UPDATE marketplace_listings SET hidden_at=now() WHERE course_id=$1 AND hidden_at IS NULL RETURNING 1) SELECT EXISTS (SELECT 1 FROM u)`, target.id).Scan(&hidden); err != nil {
				return res, err
			}
			newlyHidden = hidden
			if _, err = tx.Exec(ctx, `INSERT INTO moderation_queue_items(target_type,target_id,severity,report_count) VALUES('course',$1,$2,$3)
 ON CONFLICT (target_type,target_id) WHERE status='open' DO UPDATE SET severity=EXCLUDED.severity,report_count=EXCLUDED.report_count`, target.id, QueueSeverity(reasons, s.cfg), distinct); err != nil {
				return res, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return res, err
	}
	if newlyHidden {
		s.invalidateCatalogue(ctx)
		s.emit(anonymousID, "course_hidden_by_reports", map[string]any{"course_id": target.course, "reporter_count": distinct})
	}
	if !res.Merged {
		props := map[string]any{"report_id": reportID, "reason": in.Reason, "target_type": in.TargetType}
		if target.course != "" {
			props["course_id"] = target.course
		}
		s.emit(actor, "course_reported", props)
	}
	return res, nil
}

// ModerationQueue lists open items by severity, then age, then id (a stable tiebreak).
func (s *Service) ModerationQueue(ctx context.Context, limit int) ([]QueueItem, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text,target_type,target_id::text,severity,report_count,opened_at FROM moderation_queue_items
 WHERE status='open' ORDER BY severity DESC,opened_at,id LIMIT $1`, min(max(limit, 1), 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueItem{}
	for rows.Next() {
		var q QueueItem
		if err = rows.Scan(&q.ID, &q.TargetType, &q.TargetID, &q.Severity, &q.ReportCount, &q.OpenedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
