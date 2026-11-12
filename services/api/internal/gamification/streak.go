package gamification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// StreakBreak records the first missed local day that ended a run.
type StreakBreak struct {
	BrokenOn       string
	PreviousLength int
}

// StreakResult is the pure streak outcome for canonical dated events.
type StreakResult struct {
	Current, Longest int
	LastActive       string
	Breaks           []StreakBreak
}

func addDays(date string, n int) (string, error) {
	day, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return "", fmt.Errorf("invalid local date %q: %w", date, err)
	}
	return day.AddDate(0, 0, n).Format(time.DateOnly), nil
}

// ComputeStreak derives runs from ascending distinct local dates with no freezes. Every
// missed day up to and including decided is final; later days are not judged yet.
// Dates must already be validated (Postgres date text); reconcile enforces that.
func ComputeStreak(dates []string, decided string) StreakResult {
	return SimulateStreak(dates, decided, FreezeRules{}).StreakResult
}

// reconcileStreak recomputes the projection from xp_events under the learner lock.
// silent rebuilds (no analytics) are used when backfilling a lost projection.
func (s *Service) reconcileStreak(ctx context.Context, tx pgx.Tx, owner, zone string, now time.Time, silent bool) ([]pendingEvent, error) {
	today, err := LocalDate(now, zone)
	if err != nil {
		return nil, err
	}
	yesterday, err := addDays(today, -1)
	if err != nil {
		return nil, err
	}
	var storedDecided *string
	var storedLongest int
	err = tx.QueryRow(ctx, `SELECT decided_through::text,longest_days FROM streak_state WHERE learner_id=$1::uuid`, owner).Scan(&storedDecided, &storedLongest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	decided := yesterday
	if storedDecided != nil && *storedDecided > decided {
		decided = *storedDecided // a time zone move never un-decides a day
	}
	rows, err := tx.Query(ctx, `INSERT INTO streak_days(learner_id,local_date,first_event_id)
 SELECT learner_id,local_date,(array_agg(id ORDER BY earned_at,id))[1] FROM xp_events WHERE learner_id=$1::uuid GROUP BY learner_id,local_date
 ON CONFLICT(learner_id,local_date) DO NOTHING RETURNING id::text,local_date::text`, owner)
	if err != nil {
		return nil, err
	}
	type added struct{ id, date string }
	fresh := []added{}
	for rows.Next() {
		var a added
		if err = rows.Scan(&a.id, &a.date); err != nil {
			rows.Close()
			return nil, err
		}
		fresh = append(fresh, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	dateRows, err := tx.Query(ctx, `SELECT local_date::text FROM streak_days WHERE learner_id=$1::uuid ORDER BY local_date`, owner)
	if err != nil {
		return nil, err
	}
	dates := []string{}
	for dateRows.Next() {
		var d string
		if err = dateRows.Scan(&d); err != nil {
			dateRows.Close()
			return nil, err
		}
		dates = append(dates, d)
	}
	err = dateRows.Err()
	dateRows.Close()
	if err != nil {
		return nil, err
	}
	rules, err := loadFreezeRules(ctx, tx)
	if err != nil {
		return nil, err
	}
	sim := SimulateStreak(dates, decided, rules)
	res := sim.StreakResult
	events, err := applyFreezes(ctx, tx, owner, sim)
	if err != nil {
		return nil, err
	}
	for _, a := range fresh {
		events = append(events, pendingEvent{"streak_extended", map[string]any{"event_id": a.id, "local_date": a.date, "streak_length": sim.RunLength[a.date]}})
	}
	for _, b := range res.Breaks {
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO streak_breaks(learner_id,broken_on,previous_length) VALUES($1::uuid,$2::date,$3)
 ON CONFLICT(learner_id,broken_on) DO NOTHING RETURNING id::text`, owner, b.BrokenOn, b.PreviousLength).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		events = append(events, pendingEvent{"streak_broken", map[string]any{"event_id": id, "broken_on": b.BrokenOn, "previous_length": b.PreviousLength}})
	}
	var last any
	if res.LastActive != "" {
		last = res.LastActive
	}
	_, err = tx.Exec(ctx, `INSERT INTO streak_state(learner_id,current_days,longest_days,last_active_date,decided_through,updated_at)
 VALUES($1::uuid,$2,$3,$4::date,$5::date,$6) ON CONFLICT(learner_id) DO UPDATE SET current_days=EXCLUDED.current_days,longest_days=EXCLUDED.longest_days,
 last_active_date=EXCLUDED.last_active_date,decided_through=EXCLUDED.decided_through,updated_at=EXCLUDED.updated_at`,
		owner, res.Current, max(res.Longest, storedLongest), last, decided, now)
	if err != nil {
		return nil, err
	}
	if silent {
		return nil, nil
	}
	return events, nil
}

// Rollover is the durable, idempotent worker pass: it judges ended local days for
// learners whose zone has moved past their decided horizon and rebuilds lost projections.
func (s *Service) Rollover(ctx context.Context) error {
	now := s.now()
	rows, err := s.pool.Query(ctx, `SELECT s.learner_id::text,false FROM streak_state s
 JOIN learner_profiles p USING(learner_id) JOIN pg_timezone_names z ON z.name=p.time_zone
 WHERE (($1::timestamptz AT TIME ZONE z.name)::date-1)>s.decided_through
 UNION ALL
 (SELECT DISTINCT x.learner_id::text,true FROM xp_events x WHERE NOT EXISTS(SELECT 1 FROM streak_state s WHERE s.learner_id=x.learner_id) LIMIT 100)
 LIMIT 200`, now)
	if err != nil {
		return err
	}
	type target struct {
		owner  string
		silent bool
	}
	targets := []target{}
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.owner, &t.silent); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var result error
	for _, t := range targets {
		result = errors.Join(result, s.rollOne(ctx, t.owner, t.silent, now))
	}
	return result
}

func (s *Service) rollOne(ctx context.Context, owner string, silent bool, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked, zone string
	if err = tx.QueryRow(ctx, `SELECT l.id::text,COALESCE(NULLIF(p.time_zone,''),'UTC') FROM learners l JOIN learner_profiles p ON p.learner_id=l.id WHERE l.id=$1::uuid FOR UPDATE OF l`, owner).Scan(&locked, &zone); err != nil {
		return err
	}
	events, err := s.reconcileStreak(ctx, tx, owner, zone, now, silent)
	if err != nil {
		return fmt.Errorf("streak rollover for learner: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.emit(ctx, owner, events)
	return nil
}

// CalendarDay is one labelled day of the month grid.
type CalendarDay struct {
	Date  string `json:"date"`
	State string `json:"state"` // done, frozen, missed, today, pending, future, none
}

// StreakView is the learner-local streak read model.
type StreakView struct {
	CurrentDays     int            `json:"current_days"`
	LongestDays     int            `json:"longest_days"`
	TodayDone       bool           `json:"today_done"`
	LocalDate       string         `json:"local_date"`
	TimeZone        string         `json:"time_zone"`
	AsOf            time.Time      `json:"as_of"`
	RolloverPending bool           `json:"rollover_pending"`
	Month           string         `json:"month"`
	Calendar        []CalendarDay  `json:"calendar"`
	Freezes         FreezeView     `json:"freezes"`
	LastEarned      *EarnedNotice  `json:"last_earned"`
	Notices         []FreezeNotice `json:"notices"`
}

// FreezeView is the read-only freeze inventory. There is no write path: freezes are
// earned by effort and applied automatically.
type FreezeView struct {
	Held            int `json:"held"`
	Cap             int `json:"cap"`
	MilestoneDays   int `json:"milestone_days"`
	NextMilestoneIn int `json:"next_milestone_in"`
}

// EarnedNotice is the most recent granted freeze milestone.
type EarnedNotice struct {
	ReachedOn    string `json:"reached_on"`
	StreakLength int    `json:"streak_length"`
}

// FreezeNotice is a persistent dated record that a freeze covered a missed day.
type FreezeNotice struct {
	ProtectedOn  string `json:"protected_on"`
	Remaining    int    `json:"remaining"`
	StreakLength int    `json:"streak_length"`
}

var errBadMonth = errors.New("choose a month as YYYY-MM")

// Streak reads the stored projection after consuming pending completions. It never
// judges missed days itself: undecided days are reported as rollover pending.
func (s *Service) Streak(ctx context.Context, owner, month string) (StreakView, error) {
	var v StreakView
	if err := s.Consume(ctx, owner); err != nil {
		return v, err
	}
	now := s.now()
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(NULLIF(time_zone,''),'UTC') FROM learner_profiles WHERE learner_id=$1::uuid`, owner).Scan(&v.TimeZone); err != nil {
		return v, err
	}
	var err error
	if v.LocalDate, err = LocalDate(now, v.TimeZone); err != nil {
		return v, err
	}
	first := v.LocalDate[:7]
	if month != "" {
		first = month
	}
	start, err := time.Parse("2006-01", first)
	if err != nil || start.Format("2006-01") != first {
		return v, errBadMonth
	}
	v.Month = first
	yesterday, err := addDays(v.LocalDate, -1)
	if err != nil {
		return v, err
	}
	v.AsOf = now
	decided := ""
	var storedDecided *string
	err = s.pool.QueryRow(ctx, `SELECT current_days,longest_days,decided_through::text,updated_at FROM streak_state WHERE learner_id=$1::uuid`, owner).Scan(&v.CurrentDays, &v.LongestDays, &storedDecided, &v.AsOf)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return v, err
	}
	if storedDecided != nil {
		decided = *storedDecided
		v.RolloverPending = decided < yesterday
	}
	rows, err := s.pool.Query(ctx, `SELECT local_date::text FROM streak_days WHERE learner_id=$1::uuid ORDER BY local_date`, owner)
	if err != nil {
		return v, err
	}
	active := map[string]bool{}
	firstActive := ""
	for rows.Next() {
		var d string
		if err = rows.Scan(&d); err != nil {
			rows.Close()
			return v, err
		}
		if firstActive == "" {
			firstActive = d
		}
		active[d] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	v.TodayDone = active[v.LocalDate]
	frozen, err := s.loadFreezes(ctx, owner, &v)
	if err != nil {
		return v, err
	}
	v.Calendar = []CalendarDay{}
	for day := start; day.Month() == start.Month(); day = day.AddDate(0, 0, 1) {
		date := day.Format(time.DateOnly)
		state := "none"
		switch {
		case active[date]:
			state = "done"
		case date == v.LocalDate:
			state = "today"
		case date > v.LocalDate:
			state = "future"
		case firstActive == "" || date < firstActive:
			state = "none"
		case frozen[date]:
			state = "frozen"
		case date <= decided:
			state = "missed"
		default:
			state = "pending"
		}
		v.Calendar = append(v.Calendar, CalendarDay{Date: date, State: state})
	}
	return v, nil
}

func (s *Service) serveStreak(w http.ResponseWriter, r *http.Request, owner string) {
	if r.Method != http.MethodGet {
		write(w, 405, map[string]string{"error": "Method unavailable."})
		return
	}
	view, err := s.Streak(r.Context(), owner, r.URL.Query().Get("month"))
	if errors.Is(err, errBadMonth) {
		write(w, 400, map[string]string{"error": "Choose a valid month."})
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "gamification streak failed", "error", err)
		write(w, 503, map[string]string{"error": "Streak unavailable. Retry."})
		return
	}
	write(w, 200, view)
}

// loadFreezes fills the freeze inventory, latest earned milestone and dated notices,
// returning the set of dates a freeze covered.
func (s *Service) loadFreezes(ctx context.Context, owner string, v *StreakView) (map[string]bool, error) {
	if err := s.pool.QueryRow(ctx, `SELECT c.freeze_cap,c.freeze_milestone_days,COALESCE(i.held,0) FROM gamification_config c
 LEFT JOIN streak_freeze_inventory i ON i.learner_id=$1::uuid WHERE c.id`, owner).Scan(&v.Freezes.Cap, &v.Freezes.MilestoneDays, &v.Freezes.Held); err != nil {
		return nil, err
	}
	v.Freezes.NextMilestoneIn = v.Freezes.MilestoneDays - v.CurrentDays%v.Freezes.MilestoneDays
	var earned EarnedNotice
	err := s.pool.QueryRow(ctx, `SELECT reached_on::text,milestone_days FROM streak_freeze_milestones WHERE learner_id=$1::uuid AND granted ORDER BY reached_on DESC LIMIT 1`, owner).Scan(&earned.ReachedOn, &earned.StreakLength)
	if err == nil {
		v.LastEarned = &earned
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT protected_on::text,remaining,streak_length FROM streak_day_protections WHERE learner_id=$1::uuid ORDER BY protected_on DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	frozen := map[string]bool{}
	v.Notices = []FreezeNotice{}
	for rows.Next() {
		var n FreezeNotice
		if err = rows.Scan(&n.ProtectedOn, &n.Remaining, &n.StreakLength); err != nil {
			return nil, err
		}
		frozen[n.ProtectedOn] = true
		if len(v.Notices) < 3 {
			v.Notices = append(v.Notices, n)
		}
	}
	return frozen, rows.Err()
}
