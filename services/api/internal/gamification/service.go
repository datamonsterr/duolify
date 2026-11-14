// Package gamification projects authoritative lesson completions into XP.
package gamification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Rules is loaded from Postgres for every projection and goal change.
type Rules struct {
	Version                             string
	BaseXP, PerfectBonusXP, DefaultGoal int
	GoalOptions                         []int
}

// Validate rejects invalid or ambiguous award and goal configuration.
func (r Rules) Validate() error {
	if r.BaseXP < 1 || r.BaseXP > 10000 || r.PerfectBonusXP < 0 || r.PerfectBonusXP > 10000 || !r.AllowsGoal(r.DefaultGoal) {
		return errors.New("invalid gamification config")
	}
	seen := map[int]bool{}
	for _, goal := range r.GoalOptions {
		if goal < 1 || goal > 10000 || seen[goal] {
			return errors.New("invalid goal options")
		}
		seen[goal] = true
	}
	return nil
}

// Award returns the configured first-completion award.
func (r Rules) Award(perfect bool) int {
	if perfect {
		return r.BaseXP + r.PerfectBonusXP
	}
	return r.BaseXP
}

// AllowsGoal checks the configured goal choices.
func (r Rules) AllowsGoal(goal int) bool {
	for _, option := range r.GoalOptions {
		if option == goal {
			return true
		}
	}
	return false
}

// LocalDate applies IANA timezone rules at the completion instant.
func LocalDate(at time.Time, zone string) (string, error) {
	location, err := time.LoadLocation(zone)
	if err != nil || zone == "" || zone == "Local" {
		return "", fmt.Errorf("invalid learner timezone %q", zone)
	}
	return at.In(location).Format(time.DateOnly), nil
}

// Service consumes E4 completions and serves authenticated progress.
type Service struct {
	pool         *pgxpool.Pool
	events       analytics.Events
	rdb          *redis.Client
	now          func() time.Time
	entitlements Entitlements
	onSettled    func(ctx context.Context, owner string)
}

// OnSettled registers a callback run after a learner's streak projection commits (E6-06
// settles Friend streaks there). It must be idempotent and must not fail the caller.
func (s *Service) OnSettled(fn func(ctx context.Context, owner string)) { s.onSettled = fn }

func (s *Service) settled(ctx context.Context, owner string) {
	if s.onSettled != nil {
		s.onSettled(ctx, owner)
	}
}

// New creates a consumer with the central fail-closed consent gate.
func New(pool *pgxpool.Pool, events analytics.Events) *Service {
	if events != nil {
		events = analytics.WithConsent(events, analytics.DatabaseConsent(pool))
	}
	return &Service{pool: pool, events: events, now: time.Now}
}

// Progress is the authoritative learner-local XP projection.
type Progress struct {
	TotalXP     int64  `json:"total_xp"`
	DailyXP     int64  `json:"daily_xp"`
	DailyGoal   int    `json:"daily_goal"`
	GoalOptions []int  `json:"goal_options"`
	GoalMet     bool   `json:"goal_met"`
	LocalDate   string `json:"local_date"`
	TimeZone    string `json:"time_zone"`
}
type pendingEvent struct {
	name  string
	props map[string]any
}

func loadRules(ctx context.Context, tx pgx.Tx) (Rules, error) {
	var r Rules
	err := tx.QueryRow(ctx, `SELECT version,base_xp,perfect_bonus_xp,default_goal,goal_options FROM gamification_config WHERE id`).Scan(&r.Version, &r.BaseXP, &r.PerfectBonusXP, &r.DefaultGoal, &r.GoalOptions)
	if err != nil {
		return r, err
	}
	return r, r.Validate()
}

// Consume is safe across processes and concurrently delivered completion events.
func (s *Service) Consume(ctx context.Context, owner string) error {
	_, err := s.project(ctx, owner, nil)
	return err
}

func (s *Service) project(ctx context.Context, owner string, goal *int) (Progress, error) {
	var progress Progress
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return progress, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize all dates for this learner, including different lessons crossing a goal.
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM learners WHERE id=$1::uuid FOR UPDATE`, owner).Scan(&locked); err != nil {
		return progress, err
	}
	rules, err := loadRules(ctx, tx)
	if err != nil {
		return progress, err
	}
	if goal != nil {
		if !rules.AllowsGoal(*goal) {
			return progress, errInvalidGoal
		}
		if _, err = tx.Exec(ctx, `UPDATE learner_profiles SET daily_xp=$2 WHERE learner_id=$1::uuid`, owner, *goal); err != nil {
			return progress, err
		}
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(time_zone,''),'UTC'),COALESCE(daily_xp,$2) FROM learner_profiles WHERE learner_id=$1::uuid`, owner, rules.DefaultGoal).Scan(&progress.TimeZone, &progress.DailyGoal); err != nil {
		return progress, err
	}
	progress.GoalOptions = rules.GoalOptions
	progress.LocalDate, err = LocalDate(s.now(), progress.TimeZone)
	if err != nil {
		return progress, err
	}
	rows, err := tx.Query(ctx, `SELECT o.id::text,o.lesson_id::text,o.course_id::text,o.passed_at,c.first_try_perfect,c.duration_ms
 FROM lesson_completion_outbox o JOIN lesson_completion_summaries c USING(learner_id,lesson_id)
 WHERE o.learner_id=$1::uuid AND o.state='passed' AND NOT EXISTS(SELECT 1 FROM xp_events x WHERE x.learner_id=o.learner_id AND x.lesson_id=o.lesson_id)
 ORDER BY o.passed_at,o.id`, owner)
	if err != nil {
		return progress, err
	}
	type completion struct {
		id, lesson, course string
		at                 time.Time
		perfect            bool
		durationMS         int64
	}
	completions := []completion{}
	for rows.Next() {
		var c completion
		if err = rows.Scan(&c.id, &c.lesson, &c.course, &c.at, &c.perfect, &c.durationMS); err != nil {
			rows.Close()
			return progress, err
		}
		completions = append(completions, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return progress, err
	}
	events := []pendingEvent{}
	joined := []leagueSlot{}
	dates := map[string]bool{progress.LocalDate: true}
	for _, c := range completions {
		date, dateErr := LocalDate(c.at, progress.TimeZone)
		if dateErr != nil {
			return progress, dateErr
		}
		dates[date] = true
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO xp_events(learner_id,lesson_id,course_id,completion_id,amount,perfect,config_version,earned_at,local_date,time_zone)
  VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8,$9::date,$10) ON CONFLICT(learner_id,lesson_id) DO NOTHING RETURNING id::text`, owner, c.lesson, c.course, c.id, rules.Award(c.perfect), c.perfect, rules.Version, c.at, date, progress.TimeZone).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return progress, err
		}
		// First eligible completion of the week joins its league in the same transaction.
		slot, joinErr := s.joinLeague(ctx, tx, owner, date)
		if joinErr != nil {
			return progress, joinErr
		}
		joined = append(joined, slot)
		props := map[string]any{"event_id": id, "lesson_id": c.lesson, "course_id": c.course, "amount": rules.Award(c.perfect), "xp": rules.Award(c.perfect), "perfect": c.perfect, "duration_ms": c.durationMS, "local_date": date, "config_version": rules.Version}
		events = append(events, pendingEvent{"xp_earned", props}, pendingEvent{"lesson_completed", props})
	}
	for date := range dates {
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO daily_goal_achievements(learner_id,local_date,goal_xp)
  SELECT $1::uuid,$2::date,$3::integer WHERE (SELECT COALESCE(sum(amount),0) FROM xp_events WHERE learner_id=$1::uuid AND local_date=$2::date)>=$3::integer
  ON CONFLICT(learner_id,local_date) DO NOTHING RETURNING id::text`, owner, date, progress.DailyGoal).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return progress, err
		}
		events = append(events, pendingEvent{"daily_goal_met", map[string]any{"event_id": id, "local_date": date, "goal_xp": progress.DailyGoal}})
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0),COALESCE(sum(amount) FILTER(WHERE local_date=$2::date),0) FROM xp_events WHERE learner_id=$1::uuid`, owner, progress.LocalDate).Scan(&progress.TotalXP, &progress.DailyXP)
	if err != nil {
		return progress, err
	}
	progress.GoalMet = progress.DailyXP >= int64(progress.DailyGoal)
	if len(completions) > 0 {
		// Same transaction: a completion and its streak day commit or fail together.
		streakEvents, streakErr := s.reconcileStreak(ctx, tx, owner, progress.TimeZone, s.now(), false)
		if streakErr != nil {
			return progress, streakErr
		}
		events = append(events, streakEvents...)
	}
	if err = tx.Commit(ctx); err != nil {
		return progress, err
	}
	s.refreshLeague(ctx, owner, joined)
	s.emit(ctx, owner, events)
	s.settled(ctx, owner)
	return progress, nil
}

// emit sends derived, consent-gated notifications; durable state never depends on PostHog.
func (s *Service) emit(ctx context.Context, owner string, events []pendingEvent) {
	if s.events == nil {
		return
	}
	var captureErr error
	for _, event := range events {
		event.props["source"] = "api"
		event.props["platform"] = "web"
		event.props["app_version"] = "0.1.0"
		captureErr = errors.Join(captureErr, s.events.Capture(owner, event.name, event.props))
	}
	if captureErr != nil {
		slog.ErrorContext(ctx, "gamification analytics delivery failed", "error", captureErr)
	}
}

var errInvalidGoal = errors.New("choose a configured daily goal")

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		write(w, 401, map[string]string{"error": "Sign in required."})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/v1/gamification/analytics" {
		if r.Method != http.MethodGet {
			write(w, 405, map[string]string{"error": "Method unavailable."})
			return
		}
		s.serveAnalytics(w, r, owner)
		return
	}
	if r.URL.Path == "/v1/gamification/export/history" {
		if r.Method != http.MethodGet {
			write(w, 405, map[string]string{"error": "Method unavailable."})
			return
		}
		s.serveExportHistory(w, r, owner)
		return
	}
	if r.URL.Path == "/v1/gamification/export/analytics" {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			write(w, 405, map[string]string{"error": "Method unavailable."})
			return
		}
		s.serveExportAnalytics(w, r, owner)
		return
	}
	if r.URL.Path == "/v1/gamification/league" {
		s.serveLeague(w, r, owner)
		return
	}
	if r.URL.Path == "/v1/gamification/streak" {
		s.serveStreak(w, r, owner)
		return
	}
	if r.URL.Path == "/v1/gamification" {
		var goal *int
		if r.Method == http.MethodPut {
			var in struct {
				DailyGoal int `json:"daily_goal"`
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1024)
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			var extra any
			if err := decoder.Decode(&in); err != nil || !errors.Is(decoder.Decode(&extra), io.EOF) {
				write(w, 400, map[string]string{"error": "Choose a configured daily goal."})
				return
			}
			goal = &in.DailyGoal
		} else if r.Method != http.MethodGet {
			write(w, 405, map[string]string{"error": "Method unavailable."})
			return
		}
		progress, err := s.project(r.Context(), owner, goal)
		if errors.Is(err, errInvalidGoal) {
			write(w, 400, map[string]string{"error": "Choose a configured daily goal."})
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "gamification progress failed", "error", err)
			write(w, 503, map[string]string{"error": "Progress unavailable. Retry."})
			return
		}
		write(w, 200, progress)
		return
	}
	lesson := strings.TrimPrefix(r.URL.Path, "/v1/gamification/lessons/")
	if r.Method != http.MethodGet || lesson == r.URL.Path || strings.Contains(lesson, "/") {
		write(w, 404, map[string]string{"error": "Progress not found."})
		return
	}
	// Check ownership before consuming anything; the caller cannot probe other learners.
	var exists bool
	err := s.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM course_lesson_stubs WHERE id::text=$1 AND learner_id=$2::uuid)`, lesson, owner).Scan(&exists)
	if err != nil {
		write(w, 503, map[string]string{"error": "Progress unavailable. Retry."})
		return
	}
	if !exists {
		write(w, 404, map[string]string{"error": "Progress not found."})
		return
	}
	if err = s.Consume(r.Context(), owner); err != nil {
		write(w, 503, map[string]string{"error": "Progress unavailable. Retry."})
		return
	}
	var xp int
	var perfect bool
	err = s.pool.QueryRow(r.Context(), `SELECT amount,perfect FROM xp_events WHERE learner_id=$1::uuid AND lesson_id::text=$2`, owner, lesson).Scan(&xp, &perfect)
	if errors.Is(err, pgx.ErrNoRows) {
		write(w, 409, map[string]string{"error": "Completion is waiting to sync."})
		return
	}
	if err != nil {
		write(w, 503, map[string]string{"error": "Progress unavailable. Retry."})
		return
	}
	write(w, 200, map[string]any{"xp": xp, "perfect": perfect})
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("gamification response failed", "error", err)
	}
}

// Maintain resumes committed E4 outbox work without requiring a client read.
func (s *Service) Maintain(ctx context.Context, logger *slog.Logger) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "completion XP sweep failed", "error", err)
		}
		if err := s.Rollover(ctx); err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "streak rollover failed", "error", err)
		}
		if err := s.FinalizeLeagues(ctx); err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "league finalization failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Sweep resumes a bounded batch of learners with unconsumed completions.
func (s *Service) Sweep(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT o.learner_id::text FROM lesson_completion_outbox o JOIN lesson_completion_summaries c USING(learner_id,lesson_id) WHERE NOT EXISTS(SELECT 1 FROM xp_events x WHERE x.learner_id=o.learner_id AND x.lesson_id=o.lesson_id) LIMIT 100`)
	if err != nil {
		return err
	}
	owners := []string{}
	for rows.Next() {
		var owner string
		if err = rows.Scan(&owner); err != nil {
			rows.Close()
			return err
		}
		owners = append(owners, owner)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var result error
	for _, owner := range owners {
		result = errors.Join(result, s.Consume(ctx, owner))
	}
	return result
}
