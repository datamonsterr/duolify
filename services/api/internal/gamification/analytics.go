package gamification

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
)

// Entitlements exposes funded subscription snapshot verification.
type Entitlements interface {
	FundedPlan(ctx context.Context, owner string, at time.Time) (*credits.FundedPlan, error)
}

// UseEntitlements attaches subscription entitlement checks.
func (s *Service) UseEntitlements(e Entitlements) {
	s.entitlements = e
}

// Benefit names come from the versioned plan catalogue snapshot stored on the funded term.
// Only an active, unrevoked term whose benefits list the exact name entitles; plan ids and
// names never grant anything by themselves.
const (
	benefitHistoryExport = "Learning history export"
	benefitAnalytics     = "Advanced learning analytics"
	benefitCustomExport  = "Custom learning analytics exports"
	benefitNoUpsell      = "No upsell banners"
)

// exportRowCap bounds every streamed list; a longer list is cut and flagged `<key>_truncated`.
var exportRowCap = 50000

const (
	analyticsCourseLimit = 200
	analyticsDailyLimit  = 60
)

func hasBenefit(plan *credits.FundedPlan, name string) bool {
	if plan == nil || !plan.Active || plan.Revoked {
		return false
	}
	for _, b := range plan.Benefits {
		if strings.EqualFold(b, name) {
			return true
		}
	}
	return false
}

func isEntitledToHistoryExport(plan *credits.FundedPlan) bool {
	return hasBenefit(plan, benefitHistoryExport)
}
func isEntitledToAnalytics(plan *credits.FundedPlan) bool { return hasBenefit(plan, benefitAnalytics) }
func isEntitledToCustomExport(plan *credits.FundedPlan) bool {
	return hasBenefit(plan, benefitCustomExport)
}

var exportDimensionOrder = []string{"course", "daily", "streak", "completions", "leagues"}

// normalizeExportDimensions validates against the fixed allowlist and returns the
// de-duplicated dimensions in canonical order. Nothing client-supplied reaches SQL.
func normalizeExportDimensions(dims []string) ([]string, error) {
	if len(dims) == 0 {
		return nil, errors.New("choose at least one export dimension: course, daily, streak, completions, leagues")
	}
	want := map[string]bool{}
	for _, d := range dims {
		trimmed := strings.ToLower(strings.TrimSpace(d))
		known := false
		for _, k := range exportDimensionOrder {
			known = known || k == trimmed
		}
		if !known {
			return nil, fmt.Errorf("invalid export dimension %q; allowed dimensions are: course, daily, streak, completions, leagues", d)
		}
		want[trimmed] = true
	}
	out := make([]string, 0, len(want))
	for _, k := range exportDimensionOrder {
		if want[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

func validateExportDimensions(dims []string) error {
	_, err := normalizeExportDimensions(dims)
	return err
}

func (s *Service) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Service) getPlan(ctx context.Context, owner string) (*credits.FundedPlan, error) {
	if s.entitlements != nil {
		return s.entitlements.FundedPlan(ctx, owner, s.currentTime())
	}
	if s.pool == nil {
		return nil, nil
	}
	// Fallback reads the same E7 verified subscription tables when no service was injected.
	creditSvc := credits.NewServiceWithCatalogue(s.pool, s.rdb, nil, s.events)
	return creditSvc.FundedPlan(ctx, owner, s.currentTime())
}

// entitled loads the plan and answers 503 (lookup failed) or 403 (with a machine code and
// whether upsell copy is suppressed by the plan's No-upsell benefit). Expired, revoked and
// missing plans are denied; past-due grace is whatever E7 reports as Active.
func (s *Service) entitled(w http.ResponseWriter, r *http.Request, owner, need, message string, check func(*credits.FundedPlan) bool) (*credits.FundedPlan, bool) {
	plan, err := s.getPlan(r.Context(), owner)
	if err != nil {
		slog.Error("gamification entitlement lookup failed", "error", err)
		write(w, 503, map[string]string{"error": "Entitlements unavailable. Retry."})
		return nil, false
	}
	if check(plan) {
		return plan, true
	}
	code := "subscription_required"
	if plan != nil && (!plan.Active || plan.Revoked) {
		code = "subscription_expired"
		message = "Subscription expired. Renew to use " + need + "."
	}
	write(w, 403, map[string]any{"error": message, "code": code, "upsell_suppressed": hasBenefit(plan, benefitNoUpsell)})
	return nil, false
}

// ownedCourse joins a course title only when the course is the caller's own or one they are
// enrolled in; XP of deleted or no-longer-visible courses stays counted under a generic title.
const ownedCourse = `LEFT JOIN courses c ON c.id = x.course_id AND (c.learner_id = $1::uuid OR EXISTS(SELECT 1 FROM course_enrollments e WHERE e.course_id = c.id AND e.learner_id = $1::uuid))`

const courseAggregateSQL = `
	SELECT x.course_id::text,
	       COALESCE(c.title, 'Untitled Course'),
	       COUNT(DISTINCT x.lesson_id),
	       COALESCE(SUM(x.amount), 0),
	       COUNT(*) FILTER (WHERE x.perfect),
	       COALESCE(SUM(cs.duration_ms), 0),
	       MAX(x.earned_at)
	FROM xp_events x ` + ownedCourse + `
	LEFT JOIN lesson_completion_summaries cs ON cs.learner_id = x.learner_id AND cs.lesson_id = x.lesson_id
	WHERE x.learner_id = $1::uuid
	GROUP BY x.course_id, c.title
	ORDER BY MAX(x.earned_at) DESC, x.course_id
	LIMIT $2`

const dailyAggregateSQL = `
	SELECT x.local_date,
	       COALESCE(SUM(x.amount), 0),
	       COUNT(DISTINCT x.lesson_id),
	       EXISTS(SELECT 1 FROM daily_goal_achievements d WHERE d.learner_id = x.learner_id AND d.local_date = x.local_date)
	FROM xp_events x
	WHERE x.learner_id = $1::uuid
	GROUP BY x.learner_id, x.local_date
	ORDER BY x.local_date DESC
	LIMIT $2`

const completionsSQL = `
	SELECT x.lesson_id::text, x.course_id::text, COALESCE(c.title, 'Untitled Course'),
	       x.amount, x.perfect, x.earned_at, x.local_date, x.time_zone, COALESCE(cs.duration_ms, 0)
	FROM xp_events x ` + ownedCourse + `
	LEFT JOIN lesson_completion_summaries cs ON cs.learner_id = x.learner_id AND cs.lesson_id = x.lesson_id
	WHERE x.learner_id = $1::uuid
	ORDER BY x.earned_at DESC, x.id
	LIMIT $2`

const streakDaysSQL = `SELECT local_date FROM streak_days WHERE learner_id=$1::uuid ORDER BY local_date ASC LIMIT $2`
const protectionsSQL = `SELECT protected_on, remaining, streak_length FROM streak_day_protections WHERE learner_id=$1::uuid ORDER BY protected_on DESC LIMIT $2`
const leaguesSQL = `SELECT week_start, tier, rank, weekly_xp, outcome FROM league_week_results WHERE learner_id = $1::uuid ORDER BY week_start DESC LIMIT $2`

// each runs one owner-scoped query and reports any query, scan or iteration error.
func (s *Service) each(ctx context.Context, sql, owner string, limit int, scan func(pgx.Rows) error) error {
	rows, err := s.pool.Query(ctx, sql, owner, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func optional(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func (s *Service) streakCounters(ctx context.Context, owner string) (current, longest, held int, err error) {
	if err = optional(s.pool.QueryRow(ctx, `SELECT current_days, longest_days FROM streak_state WHERE learner_id=$1::uuid`, owner).Scan(&current, &longest)); err != nil {
		return
	}
	err = optional(s.pool.QueryRow(ctx, `SELECT held FROM streak_freeze_inventory WHERE learner_id=$1::uuid`, owner).Scan(&held))
	return
}

func scanProtection(rows pgx.Rows) (StreakProtectionNotice, error) {
	var p StreakProtectionNotice
	var d time.Time
	if err := rows.Scan(&d, &p.Remaining, &p.StreakLength); err != nil {
		return p, err
	}
	p.ProtectedOn = d.Format(time.DateOnly)
	return p, nil
}

// AnalyticsSummary summarizes top-level metrics for the learner.
type AnalyticsSummary struct {
	TotalXP               int64 `json:"total_xp"`
	TotalLessonsCompleted int64 `json:"total_lessons_completed"`
	PerfectCompletions    int64 `json:"perfect_completions"`
	TotalDurationMS       int64 `json:"total_duration_ms"`
	CurrentStreak         int   `json:"current_streak"`
	LongestStreak         int   `json:"longest_streak"`
	FreezesHeld           int   `json:"freezes_held"`
	FreezesUsed           int   `json:"freezes_used"`
}

// CourseBreakdownItem summarizes progress per course.
type CourseBreakdownItem struct {
	CourseID           string    `json:"course_id"`
	Title              string    `json:"title"`
	CompletedLessons   int       `json:"completed_lessons"`
	TotalXP            int       `json:"total_xp"`
	PerfectCompletions int       `json:"perfect_completions"`
	TotalDurationMS    int64     `json:"total_duration_ms"`
	LastCompletedAt    time.Time `json:"last_completed_at"`
}

// DailyXPBreakdownItem summarizes daily XP earnings.
type DailyXPBreakdownItem struct {
	LocalDate        string `json:"local_date"`
	XP               int    `json:"xp"`
	LessonsCompleted int    `json:"lessons_completed"`
	DailyGoalMet     bool   `json:"daily_goal_met"`
}

// StreakProtectionNotice summarizes recent freeze protection activity.
type StreakProtectionNotice struct {
	ProtectedOn  string `json:"protected_on"`
	Remaining    int    `json:"remaining"`
	StreakLength int    `json:"streak_length"`
}

// StreakBreakdown provides detailed streak and protection analytics.
type StreakBreakdown struct {
	CurrentStreak     int                      `json:"current_streak"`
	LongestStreak     int                      `json:"longest_streak"`
	FreezesHeld       int                      `json:"freezes_held"`
	FreezesUsed       int                      `json:"freezes_used"`
	RecentProtections []StreakProtectionNotice `json:"recent_protections"`
}

// CompletionBreakdown provides completion rate breakdowns.
type CompletionBreakdown struct {
	TotalCompletions   int64      `json:"total_completions"`
	PerfectCompletions int64      `json:"perfect_completions"`
	TotalDurationMS    int64      `json:"total_duration_ms"`
	AverageDurationMS  int64      `json:"average_duration_ms"`
	FirstCompletedAt   *time.Time `json:"first_completed_at,omitempty"`
	LastCompletedAt    *time.Time `json:"last_completed_at,omitempty"`
}

// AnalyticsResponse is the top-level payload for gamification analytics.
type AnalyticsResponse struct {
	PlanID              string                 `json:"plan_id"`
	PlanName            string                 `json:"plan_name"`
	EntitlementVersion  string                 `json:"entitlement_version"`
	TimeZone            string                 `json:"time_zone"`
	LocalDate           string                 `json:"local_date"`
	AsOf                string                 `json:"as_of"`
	CustomExportAllowed bool                   `json:"custom_export_allowed"`
	Summary             AnalyticsSummary       `json:"summary"`
	CourseBreakdown     []CourseBreakdownItem  `json:"course_breakdown"`
	DailyXPBreakdown    []DailyXPBreakdownItem `json:"daily_xp_breakdown"`
	StreakBreakdown     StreakBreakdown        `json:"streak_breakdown"`
	CompletionBreakdown CompletionBreakdown    `json:"completion_breakdown"`
}

func (s *Service) serveAnalytics(w http.ResponseWriter, r *http.Request, owner string) {
	ctx := r.Context()
	plan, ok := s.entitled(w, r, owner, "learning analytics", "Advanced learning analytics requires an active Plus or Pro subscription.", isEntitledToAnalytics)
	if !ok {
		return
	}
	fail := func(err error) {
		slog.Error("gamification analytics failed", "error", err)
		write(w, 503, map[string]string{"error": "Learning analytics unavailable. Retry."})
	}

	timeZone := "UTC"
	if err := optional(s.pool.QueryRow(ctx, `SELECT COALESCE(NULLIF(time_zone,''),'UTC') FROM learner_profiles WHERE learner_id=$1::uuid`, owner).Scan(&timeZone)); err != nil {
		fail(err)
		return
	}
	localDate, err := LocalDate(s.currentTime(), timeZone)
	if err != nil {
		timeZone = "UTC"
		localDate = s.currentTime().UTC().Format(time.DateOnly)
	}

	resp := AnalyticsResponse{
		PlanID:              plan.PlanID,
		PlanName:            plan.Name,
		EntitlementVersion:  plan.EntitlementVersion,
		TimeZone:            timeZone,
		LocalDate:           localDate,
		AsOf:                s.currentTime().UTC().Format(time.RFC3339),
		CustomExportAllowed: isEntitledToCustomExport(plan),
		CourseBreakdown:     []CourseBreakdownItem{},
		DailyXPBreakdown:    []DailyXPBreakdownItem{},
	}
	resp.StreakBreakdown.RecentProtections = []StreakProtectionNotice{}

	if err = s.each(ctx, courseAggregateSQL, owner, analyticsCourseLimit, func(rows pgx.Rows) error {
		var item CourseBreakdownItem
		if err := rows.Scan(&item.CourseID, &item.Title, &item.CompletedLessons, &item.TotalXP, &item.PerfectCompletions, &item.TotalDurationMS, &item.LastCompletedAt); err != nil {
			return err
		}
		resp.CourseBreakdown = append(resp.CourseBreakdown, item)
		return nil
	}); err != nil {
		fail(err)
		return
	}
	if err = s.each(ctx, dailyAggregateSQL, owner, analyticsDailyLimit, func(rows pgx.Rows) error {
		var item DailyXPBreakdownItem
		var d time.Time
		if err := rows.Scan(&d, &item.XP, &item.LessonsCompleted, &item.DailyGoalMet); err != nil {
			return err
		}
		item.LocalDate = d.Format(time.DateOnly)
		resp.DailyXPBreakdown = append(resp.DailyXPBreakdown, item)
		return nil
	}); err != nil {
		fail(err)
		return
	}

	sb := &resp.StreakBreakdown
	if sb.CurrentStreak, sb.LongestStreak, sb.FreezesHeld, err = s.streakCounters(ctx, owner); err != nil {
		fail(err)
		return
	}
	if err = s.each(ctx, `SELECT protected_on, remaining, streak_length FROM streak_day_protections WHERE learner_id=$1::uuid ORDER BY protected_on DESC LIMIT $2`, owner, 5, func(rows pgx.Rows) error {
		p, err := scanProtection(rows)
		if err == nil {
			sb.RecentProtections = append(sb.RecentProtections, p)
		}
		return err
	}); err != nil {
		fail(err)
		return
	}
	if err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM streak_day_protections WHERE learner_id=$1::uuid`, owner).Scan(&sb.FreezesUsed); err != nil {
		fail(err)
		return
	}

	cb := &resp.CompletionBreakdown
	if err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE x.perfect),
		       COALESCE(SUM(cs.duration_ms), 0)::bigint,
		       COALESCE(AVG(cs.duration_ms), 0)::bigint,
		       MIN(x.earned_at),
		       MAX(x.earned_at),
		       COALESCE(SUM(x.amount), 0)::bigint
		FROM xp_events x
		LEFT JOIN lesson_completion_summaries cs ON cs.learner_id = x.learner_id AND cs.lesson_id = x.lesson_id
		WHERE x.learner_id = $1::uuid`, owner).Scan(&cb.TotalCompletions, &cb.PerfectCompletions, &cb.TotalDurationMS, &cb.AverageDurationMS, &cb.FirstCompletedAt, &cb.LastCompletedAt, &resp.Summary.TotalXP); err != nil {
		fail(err)
		return
	}
	resp.Summary.TotalLessonsCompleted = cb.TotalCompletions
	resp.Summary.PerfectCompletions = cb.PerfectCompletions
	resp.Summary.TotalDurationMS = cb.TotalDurationMS
	resp.Summary.CurrentStreak = sb.CurrentStreak
	resp.Summary.LongestStreak = sb.LongestStreak
	resp.Summary.FreezesHeld = sb.FreezesHeld
	resp.Summary.FreezesUsed = sb.FreezesUsed

	write(w, 200, resp)
}

// HistoryItem is one completed lesson in the learner's own history export.
type HistoryItem struct {
	LessonID    string    `json:"lesson_id"`
	CourseID    string    `json:"course_id"`
	CourseTitle string    `json:"course_title"`
	XP          int       `json:"xp"`
	Perfect     bool      `json:"perfect"`
	EarnedAt    time.Time `json:"earned_at"`
	LocalDate   string    `json:"local_date"`
	TimeZone    string    `json:"time_zone"`
	DurationMS  int64     `json:"duration_ms"`
}

func scanHistory(rows pgx.Rows) (any, error) {
	var item HistoryItem
	var d time.Time
	if err := rows.Scan(&item.LessonID, &item.CourseID, &item.CourseTitle, &item.XP, &item.Perfect, &item.EarnedAt, &d, &item.TimeZone, &item.DurationMS); err != nil {
		return nil, err
	}
	item.EarnedAt = item.EarnedAt.UTC()
	item.LocalDate = d.Format(time.DateOnly)
	return item, nil
}

// exportStream writes one JSON document incrementally through a small buffer, so memory stays
// bounded. Failure before the first byte reaches the client becomes a clean 503; later it
// aborts the connection so a cut file can never parse as complete.
type exportStream struct {
	w       http.ResponseWriter
	bw      *bufio.Writer
	enc     *json.Encoder
	started bool
	first   bool
	err     error
}

type flagWriter struct {
	w       io.Writer
	started *bool
}

func (f flagWriter) Write(p []byte) (int, error) {
	*f.started = true
	return f.w.Write(p)
}

func newExportStream(w http.ResponseWriter, filename string) *exportStream {
	e := &exportStream{w: w, first: true}
	e.bw = bufio.NewWriterSize(flagWriter{w: w, started: &e.started}, 32<<10)
	e.enc = json.NewEncoder(e.bw)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	return e
}

func (e *exportStream) raw(s string) {
	if e.err == nil {
		_, e.err = e.bw.WriteString(s)
	}
}

func (e *exportStream) value(v any) {
	if e.err == nil {
		e.err = e.enc.Encode(v)
	}
}

func (e *exportStream) field(name string, v any) {
	e.sep()
	e.value(name)
	e.raw(":")
	e.value(v)
}

func (e *exportStream) sep() {
	if !e.first {
		e.raw(",")
	}
	e.first = false
}

// list streams `"name":[...]` plus `"name_truncated":true` when the cap cut it.
func (e *exportStream) list(s *Service, ctx context.Context, name, sql, owner string, scan func(pgx.Rows) (any, error)) {
	if e.err != nil {
		return
	}
	e.sep()
	e.value(name)
	e.raw(":[")
	n := 0
	e.err = s.each(ctx, sql, owner, exportRowCap+1, func(rows pgx.Rows) error {
		if n == exportRowCap {
			n++
			return nil
		}
		if n > exportRowCap {
			return nil
		}
		v, err := scan(rows)
		if err != nil {
			return err
		}
		if n > 0 {
			e.raw(",")
		}
		n++
		e.value(v)
		return e.err
	})
	e.raw("]")
	if n > exportRowCap {
		e.field(name+"_truncated", true)
	}
}

func (e *exportStream) finish(what string) {
	if e.err == nil {
		e.err = e.bw.Flush()
	}
	if e.err == nil {
		return
	}
	slog.Error("gamification export failed", "export", what, "error", e.err)
	if e.started {
		panic(http.ErrAbortHandler)
	}
	h := e.w.Header()
	h.Del("Content-Disposition")
	write(e.w, 503, map[string]string{"error": "Could not prepare export. Retry."})
}

func (s *Service) streakSection(e *exportStream, ctx context.Context, owner, daysKey string, protections bool) {
	if e.err != nil {
		return
	}
	cur, longest, held, err := s.streakCounters(ctx, owner)
	if err != nil {
		e.err = err
		return
	}
	e.raw("{")
	inner := &exportStream{w: e.w, bw: e.bw, enc: e.enc, first: true}
	inner.field("current_streak", cur)
	inner.field("longest_streak", longest)
	inner.field("freezes_held", held)
	inner.list(s, ctx, daysKey, streakDaysSQL, owner, func(rows pgx.Rows) (any, error) {
		var d time.Time
		err := rows.Scan(&d)
		return d.Format(time.DateOnly), err
	})
	if protections {
		inner.list(s, ctx, "protections", protectionsSQL, owner, func(rows pgx.Rows) (any, error) { return scanProtection(rows) })
	}
	e.raw("}")
	e.err = inner.err
}

func (s *Service) preamble(e *exportStream, owner string, plan *credits.FundedPlan) {
	e.raw("{")
	e.field("schema_version", 1)
	e.field("exported_at", s.currentTime().UTC().Format(time.RFC3339))
	e.field("learner_id", owner)
	e.field("plan_id", plan.PlanID)
	e.field("entitlement_version", plan.EntitlementVersion)
}

func (s *Service) serveExportHistory(w http.ResponseWriter, r *http.Request, owner string) {
	ctx := r.Context()
	plan, ok := s.entitled(w, r, owner, "learning history export", "Learning history export requires an active Learn, Plus, or Pro subscription.", isEntitledToHistoryExport)
	if !ok {
		return
	}
	e := newExportStream(w, "duolify-learning-history.json")
	s.preamble(e, owner, plan)
	e.list(s, ctx, "history", completionsSQL, owner, scanHistory)
	e.sep()
	e.value("streaks")
	e.raw(":")
	s.streakSection(e, ctx, owner, "days", true)
	e.raw("}\n")
	e.finish("history")
}

func (s *Service) serveExportAnalytics(w http.ResponseWriter, r *http.Request, owner string) {
	ctx := r.Context()
	plan, ok := s.entitled(w, r, owner, "custom analytics export", "Custom analytics export requires an active Pro subscription.", isEntitledToCustomExport)
	if !ok {
		return
	}

	var requested []string
	if r.Method == http.MethodPost {
		var in struct {
			Dimensions []string `json:"dimensions"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var extra any
		if err := dec.Decode(&in); err != nil || !errors.Is(dec.Decode(&extra), io.EOF) {
			write(w, 400, map[string]string{"error": "Provide a valid JSON body with dimensions array."})
			return
		}
		requested = in.Dimensions
	} else if p := r.URL.Query().Get("dimensions"); p != "" {
		requested = strings.Split(p, ",")
	}
	dims, err := normalizeExportDimensions(requested)
	if err != nil {
		write(w, 400, map[string]string{"error": err.Error()})
		return
	}

	e := newExportStream(w, "duolify-analytics-export.json")
	s.preamble(e, owner, plan)
	e.field("dimensions", dims)
	e.sep()
	e.value("data")
	e.raw(":{")
	e.first = true
	for _, d := range dims {
		switch d {
		case "course":
			e.list(s, ctx, "course", courseAggregateSQL, owner, func(rows pgx.Rows) (any, error) {
				var cid, title string
				var completed, xp, perfect int
				var duration int64
				var last time.Time
				err := rows.Scan(&cid, &title, &completed, &xp, &perfect, &duration, &last)
				return map[string]any{"course_id": cid, "title": title, "completed_lessons": completed, "total_xp": xp, "perfect_completions": perfect, "duration_ms": duration, "last_completed_at": last.UTC().Format(time.RFC3339)}, err
			})
		case "daily":
			e.list(s, ctx, "daily", dailyAggregateSQL, owner, func(rows pgx.Rows) (any, error) {
				var d time.Time
				var xp, lessons int
				var goal bool
				err := rows.Scan(&d, &xp, &lessons, &goal)
				return map[string]any{"local_date": d.Format(time.DateOnly), "xp": xp, "lessons_completed": lessons, "daily_goal_met": goal}, err
			})
		case "streak":
			e.sep()
			e.value("streak")
			e.raw(":")
			s.streakSection(e, ctx, owner, "active_days", false)
		case "completions":
			e.list(s, ctx, "completions", completionsSQL, owner, scanHistory)
		case "leagues":
			e.list(s, ctx, "leagues", leaguesSQL, owner, func(rows pgx.Rows) (any, error) {
				var ws time.Time
				var tier, outcome string
				var rank, xp int
				err := rows.Scan(&ws, &tier, &rank, &xp, &outcome)
				return map[string]any{"week_start": ws.Format(time.DateOnly), "tier": tier, "rank": rank, "weekly_xp": xp, "outcome": outcome}, err
			})
		}
	}
	e.raw("}}\n")
	e.finish("analytics")
}
