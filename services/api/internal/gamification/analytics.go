package gamification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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

func containsBenefit(plan *credits.FundedPlan, search string) bool {
	if plan == nil {
		return false
	}
	for _, b := range plan.Benefits {
		if strings.EqualFold(b, search) {
			return true
		}
	}
	return false
}

func isEntitledToHistoryExport(plan *credits.FundedPlan) bool {
	if plan == nil || !plan.Active || plan.Revoked {
		return false
	}
	if containsBenefit(plan, "learning history export") || containsBenefit(plan, "learning-history export") {
		return true
	}
	lowerID := strings.ToLower(plan.PlanID)
	lowerName := strings.ToLower(plan.Name)
	return strings.HasPrefix(lowerID, "learn") || strings.HasPrefix(lowerID, "plus") || strings.HasPrefix(lowerID, "pro") ||
		lowerName == "learn" || lowerName == "plus" || lowerName == "pro"
}

func isEntitledToAnalytics(plan *credits.FundedPlan) bool {
	if plan == nil || !plan.Active || plan.Revoked {
		return false
	}
	if containsBenefit(plan, "advanced learning analytics") {
		return true
	}
	lowerID := strings.ToLower(plan.PlanID)
	lowerName := strings.ToLower(plan.Name)
	return strings.HasPrefix(lowerID, "plus") || strings.HasPrefix(lowerID, "pro") ||
		lowerName == "plus" || lowerName == "pro"
}

func isEntitledToCustomExport(plan *credits.FundedPlan) bool {
	if plan == nil || !plan.Active || plan.Revoked {
		return false
	}
	if containsBenefit(plan, "custom learning analytics exports") || containsBenefit(plan, "custom analytics export") || containsBenefit(plan, "custom learning analytics export") {
		return true
	}
	lowerID := strings.ToLower(plan.PlanID)
	lowerName := strings.ToLower(plan.Name)
	return strings.HasPrefix(lowerID, "pro") || lowerName == "pro"
}

var allowedExportDimensions = map[string]bool{
	"course":      true,
	"daily":       true,
	"streak":      true,
	"completions": true,
	"leagues":     true,
}

func validateExportDimensions(dims []string) error {
	if len(dims) == 0 {
		return errors.New("choose at least one export dimension: course, daily, streak, completions, leagues")
	}
	seen := make(map[string]bool)
	for _, d := range dims {
		trimmed := strings.ToLower(strings.TrimSpace(d))
		if !allowedExportDimensions[trimmed] {
			return fmt.Errorf("invalid export dimension %q; allowed dimensions are: course, daily, streak, completions, leagues", d)
		}
		if seen[trimmed] {
			continue
		}
		seen[trimmed] = true
	}
	return nil
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
	// Fallback directly to querying subscription tables if entitlements service was not injected.
	creditSvc := credits.NewServiceWithCatalogue(s.pool, s.rdb, nil, s.events)
	return creditSvc.FundedPlan(ctx, owner, s.currentTime())
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
	plan, err := s.getPlan(ctx, owner)
	if err != nil {
		write(w, 503, map[string]string{"error": "Entitlements unavailable. Retry."})
		return
	}

	if plan == nil || !plan.Active || plan.Revoked {
		if plan != nil && !plan.Active {
			write(w, 403, map[string]string{
				"error": "Subscription expired. Renew to access learning analytics.",
				"code":  "subscription_expired",
			})
			return
		}
		write(w, 403, map[string]string{
			"error": "Advanced learning analytics requires an active Plus or Pro subscription.",
			"code":  "subscription_required",
		})
		return
	}

	if !isEntitledToAnalytics(plan) {
		write(w, 403, map[string]string{
			"error": "Advanced learning analytics requires an active Plus or Pro subscription.",
			"code":  "subscription_required",
		})
		return
	}

	timeZone := "UTC"
	_ = s.pool.QueryRow(ctx, `SELECT COALESCE(NULLIF(time_zone,''),'UTC') FROM learner_profiles WHERE learner_id=$1::uuid`, owner).Scan(&timeZone)
	localDate, err := LocalDate(s.currentTime(), timeZone)
	if err != nil {
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

	// 1. Course breakdown: join xp_events with courses, strictly exclude friends' private course content.
	courseRows, err := s.pool.Query(ctx, `
		SELECT x.course_id::text,
		       COALESCE(c.title, 'Untitled Course'),
		       COUNT(DISTINCT x.lesson_id),
		       COALESCE(SUM(x.amount), 0),
		       COUNT(*) FILTER (WHERE x.perfect),
		       COALESCE(SUM(cs.duration_ms), 0),
		       MAX(x.earned_at)
		FROM xp_events x
		LEFT JOIN courses c ON c.id = x.course_id
		LEFT JOIN lesson_completion_summaries cs ON cs.learner_id = x.learner_id AND cs.lesson_id = x.lesson_id
		WHERE x.learner_id = $1::uuid
		  AND (c.learner_id = $1::uuid OR EXISTS(SELECT 1 FROM course_enrollments e WHERE e.course_id = c.id AND e.learner_id = $1::uuid))
		GROUP BY x.course_id, c.title
		ORDER BY MAX(x.earned_at) DESC
	`, owner)
	if err == nil {
		defer courseRows.Close()
		for courseRows.Next() {
			var item CourseBreakdownItem
			if err := courseRows.Scan(&item.CourseID, &item.Title, &item.CompletedLessons, &item.TotalXP, &item.PerfectCompletions, &item.TotalDurationMS, &item.LastCompletedAt); err == nil {
				resp.CourseBreakdown = append(resp.CourseBreakdown, item)
			}
		}
	}

	// 2. Daily XP breakdown
	dailyRows, err := s.pool.Query(ctx, `
		SELECT x.local_date,
		       COALESCE(SUM(x.amount), 0),
		       COUNT(DISTINCT x.lesson_id),
		       EXISTS(SELECT 1 FROM daily_goal_achievements d WHERE d.learner_id = x.learner_id AND d.local_date = x.local_date)
		FROM xp_events x
		WHERE x.learner_id = $1::uuid
		GROUP BY x.local_date
		ORDER BY x.local_date DESC
		LIMIT 60
	`, owner)
	if err == nil {
		defer dailyRows.Close()
		for dailyRows.Next() {
			var item DailyXPBreakdownItem
			var d time.Time
			if err := dailyRows.Scan(&d, &item.XP, &item.LessonsCompleted, &item.DailyGoalMet); err == nil {
				item.LocalDate = d.Format(time.DateOnly)
				resp.DailyXPBreakdown = append(resp.DailyXPBreakdown, item)
			}
		}
	}

	// 3. Streak breakdown
	_ = s.pool.QueryRow(ctx, `SELECT current_days, longest_days FROM streak_state WHERE learner_id=$1::uuid`, owner).Scan(&resp.StreakBreakdown.CurrentStreak, &resp.StreakBreakdown.LongestStreak)
	_ = s.pool.QueryRow(ctx, `SELECT held FROM streak_freeze_inventory WHERE learner_id=$1::uuid`, owner).Scan(&resp.StreakBreakdown.FreezesHeld)
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM streak_day_protections WHERE learner_id=$1::uuid`, owner).Scan(&resp.StreakBreakdown.FreezesUsed)

	protectionRows, err := s.pool.Query(ctx, `SELECT protected_on, remaining, streak_length FROM streak_day_protections WHERE learner_id=$1::uuid ORDER BY protected_on DESC LIMIT 5`, owner)
	if err == nil {
		defer protectionRows.Close()
		for protectionRows.Next() {
			var p StreakProtectionNotice
			var d time.Time
			if err := protectionRows.Scan(&d, &p.Remaining, &p.StreakLength); err == nil {
				p.ProtectedOn = d.Format(time.DateOnly)
				resp.StreakBreakdown.RecentProtections = append(resp.StreakBreakdown.RecentProtections, p)
			}
		}
	}

	// 4. Completion breakdown
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT x.lesson_id),
		       COUNT(*) FILTER (WHERE x.perfect),
		       COALESCE(SUM(cs.duration_ms), 0),
		       COALESCE(AVG(cs.duration_ms), 0),
		       MIN(x.earned_at),
		       MAX(x.earned_at)
		FROM xp_events x
		LEFT JOIN lesson_completion_summaries cs ON cs.learner_id = x.learner_id AND cs.lesson_id = x.lesson_id
		WHERE x.learner_id = $1::uuid
	`, owner).Scan(
		&resp.CompletionBreakdown.TotalCompletions,
		&resp.CompletionBreakdown.PerfectCompletions,
		&resp.CompletionBreakdown.TotalDurationMS,
		&resp.CompletionBreakdown.AverageDurationMS,
		&resp.CompletionBreakdown.FirstCompletedAt,
		&resp.CompletionBreakdown.LastCompletedAt,
	)

	// Summary totals
	_ = s.pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM xp_events WHERE learner_id=$1::uuid`, owner).Scan(&resp.Summary.TotalXP)
	resp.Summary.TotalLessonsCompleted = resp.CompletionBreakdown.TotalCompletions
	resp.Summary.PerfectCompletions = resp.CompletionBreakdown.PerfectCompletions
	resp.Summary.TotalDurationMS = resp.CompletionBreakdown.TotalDurationMS
	resp.Summary.CurrentStreak = resp.StreakBreakdown.CurrentStreak
	resp.Summary.LongestStreak = resp.StreakBreakdown.LongestStreak
	resp.Summary.FreezesHeld = resp.StreakBreakdown.FreezesHeld
	resp.Summary.FreezesUsed = resp.StreakBreakdown.FreezesUsed

	write(w, 200, resp)
}

func (s *Service) serveExportHistory(w http.ResponseWriter, r *http.Request, owner string) {
	ctx := r.Context()
	plan, err := s.getPlan(ctx, owner)
	if err != nil {
		write(w, 503, map[string]string{"error": "Entitlements unavailable. Retry."})
		return
	}

	if !isEntitledToHistoryExport(plan) {
		write(w, 403, map[string]string{
			"error": "Learning history export requires an active Learn, Plus, or Pro subscription.",
		})
		return
	}

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

	items := []HistoryItem{}
	rows, err := s.pool.Query(ctx, `
		SELECT x.lesson_id::text,
		       x.course_id::text,
		       COALESCE(c.title, 'Untitled Course'),
		       x.amount,
		       x.perfect,
		       x.earned_at,
		       x.local_date,
		       x.time_zone,
		       COALESCE(cs.duration_ms, 0)
		FROM xp_events x
		LEFT JOIN courses c ON c.id = x.course_id
		LEFT JOIN lesson_completion_summaries cs ON cs.learner_id = x.learner_id AND cs.lesson_id = x.lesson_id
		WHERE x.learner_id = $1::uuid
		  AND (c.learner_id = $1::uuid OR EXISTS(SELECT 1 FROM course_enrollments e WHERE e.course_id = c.id AND e.learner_id = $1::uuid))
		ORDER BY x.earned_at DESC
	`, owner)
	if err != nil {
		write(w, 503, map[string]string{"error": "Could not prepare learning history. Retry."})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item HistoryItem
		var d time.Time
		if err := rows.Scan(&item.LessonID, &item.CourseID, &item.CourseTitle, &item.XP, &item.Perfect, &item.EarnedAt, &d, &item.TimeZone, &item.DurationMS); err == nil {
			item.LocalDate = d.Format(time.DateOnly)
			items = append(items, item)
		}
	}

	var currentStreak, longestStreak, freezesHeld int
	_ = s.pool.QueryRow(ctx, `SELECT current_days, longest_days FROM streak_state WHERE learner_id=$1::uuid`, owner).Scan(&currentStreak, &longestStreak)
	_ = s.pool.QueryRow(ctx, `SELECT held FROM streak_freeze_inventory WHERE learner_id=$1::uuid`, owner).Scan(&freezesHeld)

	streakDays := []string{}
	sdRows, err := s.pool.Query(ctx, `SELECT local_date FROM streak_days WHERE learner_id=$1::uuid ORDER BY local_date ASC`, owner)
	if err == nil {
		defer sdRows.Close()
		for sdRows.Next() {
			var d time.Time
			if err := sdRows.Scan(&d); err == nil {
				streakDays = append(streakDays, d.Format(time.DateOnly))
			}
		}
	}

	protections := []StreakProtectionNotice{}
	prRows, err := s.pool.Query(ctx, `SELECT protected_on, remaining, streak_length FROM streak_day_protections WHERE learner_id=$1::uuid ORDER BY protected_on DESC`, owner)
	if err == nil {
		defer prRows.Close()
		for prRows.Next() {
			var p StreakProtectionNotice
			var d time.Time
			if err := prRows.Scan(&d, &p.Remaining, &p.StreakLength); err == nil {
				p.ProtectedOn = d.Format(time.DateOnly)
				protections = append(protections, p)
			}
		}
	}

	payload := map[string]any{
		"schema_version":      1,
		"exported_at":         s.currentTime().UTC().Format(time.RFC3339),
		"learner_id":          owner,
		"plan_id":             plan.PlanID,
		"entitlement_version": plan.EntitlementVersion,
		"history":             items,
		"streaks": map[string]any{
			"current_streak": currentStreak,
			"longest_streak": longestStreak,
			"freezes_held":   freezesHeld,
			"days":           streakDays,
			"protections":    protections,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="duolify-learning-history.json"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Service) serveExportAnalytics(w http.ResponseWriter, r *http.Request, owner string) {
	ctx := r.Context()
	plan, err := s.getPlan(ctx, owner)
	if err != nil {
		write(w, 503, map[string]string{"error": "Entitlements unavailable. Retry."})
		return
	}

	if !isEntitledToCustomExport(plan) {
		write(w, 403, map[string]string{
			"error": "Custom analytics export requires an active Pro subscription.",
		})
		return
	}

	var dims []string
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
		dims = in.Dimensions
	} else {
		dimParam := r.URL.Query().Get("dimensions")
		if dimParam != "" {
			dims = strings.Split(dimParam, ",")
		}
	}

	for i := range dims {
		dims[i] = strings.ToLower(strings.TrimSpace(dims[i]))
	}

	if err := validateExportDimensions(dims); err != nil {
		write(w, 400, map[string]string{"error": err.Error()})
		return
	}

	dimSet := make(map[string]bool)
	for _, d := range dims {
		dimSet[d] = true
	}

	data := make(map[string]any)

	// course dimension
	if dimSet["course"] {
		coursesList := []map[string]any{}
		rows, err := s.pool.Query(ctx, `
			SELECT x.course_id::text,
			       COALESCE(c.title, 'Untitled Course'),
			       COUNT(DISTINCT x.lesson_id),
			       COALESCE(SUM(x.amount), 0),
			       COUNT(*) FILTER (WHERE x.perfect),
			       COALESCE(SUM(cs.duration_ms), 0),
			       MAX(x.earned_at)
			FROM xp_events x
			LEFT JOIN courses c ON c.id = x.course_id
			LEFT JOIN lesson_completion_summaries cs ON cs.learner_id = x.learner_id AND cs.lesson_id = x.lesson_id
			WHERE x.learner_id = $1::uuid
			  AND (c.learner_id = $1::uuid OR EXISTS(SELECT 1 FROM course_enrollments e WHERE e.course_id = c.id AND e.learner_id = $1::uuid))
			GROUP BY x.course_id, c.title
			ORDER BY MAX(x.earned_at) DESC
		`, owner)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var cid, title string
				var completed, xp, perfect int
				var duration int64
				var lastAt time.Time
				if err := rows.Scan(&cid, &title, &completed, &xp, &perfect, &duration, &lastAt); err == nil {
					coursesList = append(coursesList, map[string]any{
						"course_id":           cid,
						"title":               title,
						"completed_lessons":   completed,
						"total_xp":            xp,
						"perfect_completions": perfect,
						"duration_ms":         duration,
						"last_completed_at":   lastAt.UTC().Format(time.RFC3339),
					})
				}
			}
		}
		data["course"] = coursesList
	}

	// daily dimension
	if dimSet["daily"] {
		dailyList := []map[string]any{}
		rows, err := s.pool.Query(ctx, `
			SELECT x.local_date,
			       COALESCE(SUM(x.amount), 0),
			       COUNT(DISTINCT x.lesson_id),
			       EXISTS(SELECT 1 FROM daily_goal_achievements d WHERE d.learner_id = x.learner_id AND d.local_date = x.local_date)
			FROM xp_events x
			WHERE x.learner_id = $1::uuid
			GROUP BY x.local_date
			ORDER BY x.local_date DESC
		`, owner)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var d time.Time
				var xp, lessons int
				var goalMet bool
				if err := rows.Scan(&d, &xp, &lessons, &goalMet); err == nil {
					dailyList = append(dailyList, map[string]any{
						"local_date":        d.Format(time.DateOnly),
						"xp":                xp,
						"lessons_completed": lessons,
						"daily_goal_met":    goalMet,
					})
				}
			}
		}
		data["daily"] = dailyList
	}

	// streak dimension
	if dimSet["streak"] {
		var currentStreak, longestStreak, freezesHeld int
		_ = s.pool.QueryRow(ctx, `SELECT current_days, longest_days FROM streak_state WHERE learner_id=$1::uuid`, owner).Scan(&currentStreak, &longestStreak)
		_ = s.pool.QueryRow(ctx, `SELECT held FROM streak_freeze_inventory WHERE learner_id=$1::uuid`, owner).Scan(&freezesHeld)

		streakDays := []string{}
		sdRows, err := s.pool.Query(ctx, `SELECT local_date FROM streak_days WHERE learner_id=$1::uuid ORDER BY local_date ASC`, owner)
		if err == nil {
			defer sdRows.Close()
			for sdRows.Next() {
				var d time.Time
				if err := sdRows.Scan(&d); err == nil {
					streakDays = append(streakDays, d.Format(time.DateOnly))
				}
			}
		}
		data["streak"] = map[string]any{
			"current_streak": currentStreak,
			"longest_streak": longestStreak,
			"freezes_held":   freezesHeld,
			"active_days":    streakDays,
		}
	}

	// completions dimension
	if dimSet["completions"] {
		completionsList := []map[string]any{}
		rows, err := s.pool.Query(ctx, `
			SELECT x.lesson_id::text,
			       x.course_id::text,
			       COALESCE(c.title, 'Untitled Course'),
			       x.amount,
			       x.perfect,
			       x.earned_at,
			       x.local_date,
			       COALESCE(cs.duration_ms, 0)
			FROM xp_events x
			LEFT JOIN courses c ON c.id = x.course_id
			LEFT JOIN lesson_completion_summaries cs ON cs.learner_id = x.learner_id AND cs.lesson_id = x.lesson_id
			WHERE x.learner_id = $1::uuid
			  AND (c.learner_id = $1::uuid OR EXISTS(SELECT 1 FROM course_enrollments e WHERE e.course_id = c.id AND e.learner_id = $1::uuid))
			ORDER BY x.earned_at DESC
		`, owner)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var lid, cid, title string
				var xp int
				var perfect bool
				var earnedAt, ld time.Time
				var dur int64
				if err := rows.Scan(&lid, &cid, &title, &xp, &perfect, &earnedAt, &ld, &dur); err == nil {
					completionsList = append(completionsList, map[string]any{
						"lesson_id":    lid,
						"course_id":    cid,
						"course_title": title,
						"xp":           xp,
						"perfect":      perfect,
						"earned_at":    earnedAt.UTC().Format(time.RFC3339),
						"local_date":   ld.Format(time.DateOnly),
						"duration_ms":  dur,
					})
				}
			}
		}
		data["completions"] = completionsList
	}

	// leagues dimension
	if dimSet["leagues"] {
		leaguesList := []map[string]any{}
		rows, err := s.pool.Query(ctx, `
			SELECT week_start, tier, rank, weekly_xp, outcome
			FROM league_week_results
			WHERE learner_id = $1::uuid
			ORDER BY week_start DESC
		`, owner)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var ws time.Time
				var tier, outcome string
				var rank, weeklyXP int
				if err := rows.Scan(&ws, &tier, &rank, &weeklyXP, &outcome); err == nil {
					leaguesList = append(leaguesList, map[string]any{
						"week_start": ws.Format(time.DateOnly),
						"tier":       tier,
						"rank":       rank,
						"weekly_xp":  weeklyXP,
						"outcome":    outcome,
					})
				}
			}
		}
		data["leagues"] = leaguesList
	}

	payload := map[string]any{
		"schema_version":      1,
		"exported_at":         s.currentTime().UTC().Format(time.RFC3339),
		"learner_id":          owner,
		"plan_id":             plan.PlanID,
		"entitlement_version": plan.EntitlementVersion,
		"dimensions":          dims,
		"data":                data,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="duolify-analytics-export.json"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}
