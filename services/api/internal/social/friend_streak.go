package social

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/gamification"
	"github.com/jackc/pgx/v5"
)

// Friend streak states shown to a friend.
const (
	stateNone          = "none"
	stateBothDone      = "both_done"
	stateWaitingYou    = "waiting_you"
	stateWaitingFriend = "waiting_friend"
	stateWaitingBoth   = "waiting_both"
	stateProtected     = "protected"
	stateChecking      = "checking"
	stateBroken        = "broken"
)

// DaySide is one friend's own local day as shown in the Friends list.
type DaySide struct {
	LocalDate string `json:"local_date"`
	TimeZone  string `json:"time_zone"`
	Done      bool   `json:"done"`
	Protected bool   `json:"protected"`
}

// FriendStreakView is the privacy-permitted shared streak: counts, state and both local days.
type FriendStreakView struct {
	Days          int        `json:"days"`
	Longest       int        `json:"longest"`
	State         string     `json:"state"`
	You           DaySide    `json:"you"`
	Friend        DaySide    `json:"friend"`
	CheckingUntil *time.Time `json:"checking_until,omitempty"`
}

// sideDays is one friend's E5 projection: zone, completion days, personal protections
// and the last local date already judged.
type sideDays struct {
	Zone              string
	Active, Protected map[string]bool
	Decided           string
}

// runState is the persisted pair state. Cursors are the last settled local date of each side.
type runState struct {
	Days, Longest, Run                         int
	CursorLow, CursorHigh, StartLow, StartHigh string
}

type settledDay struct {
	Low, High, Outcome        string
	Length, Run               int
	Started                   bool
	RunStartLow, RunStartHigh string
}

func parseDay(date, zone string) (time.Time, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, err
	}
	return time.ParseInLocation(time.DateOnly, date, loc)
}

// localDayBounds returns the real-time interval of a local date in a zone.
func localDayBounds(date, zone string) (start, end time.Time, err error) {
	if start, err = parseDay(date, zone); err != nil {
		return
	}
	return start, start.AddDate(0, 0, 1), nil
}

// datesOverlap reports whether two learners' local dates overlap in real time.
func datesOverlap(lowZone, lowDate, highZone, highDate string) bool {
	ls, le, err := localDayBounds(lowDate, lowZone)
	if err != nil {
		return false
	}
	hs, he, err := localDayBounds(highDate, highZone)
	return err == nil && ls.Before(he) && hs.Before(le)
}

func shiftDay(date string, n int) string {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return ""
	}
	return d.AddDate(0, 0, n).Format(time.DateOnly)
}

func sortedDays(m map[string]bool, after string) []string {
	out := []string{}
	for d := range m {
		if d > after {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	return out
}

// findStart returns the earliest overlapping pair of active dates after the cursors whose
// day-ends fall after the friendship began.
func findStart(st runState, low, high sideDays, since time.Time) (string, string, bool) {
	highDays := sortedDays(high.Active, st.CursorHigh)
	for _, ld := range sortedDays(low.Active, st.CursorLow) {
		ls, le, err := localDayBounds(ld, low.Zone)
		if err != nil || !le.After(since) {
			continue
		}
		for _, hd := range highDays {
			hs, he, err := localDayBounds(hd, high.Zone)
			if err != nil || !he.After(since) {
				continue
			}
			if !hs.Before(le) {
				break
			}
			if ls.Before(he) {
				return ld, hd, true
			}
		}
	}
	return "", "", false
}

// evaluatePair settles every shared day that can be decided now. A continuation day is
// extended as soon as both complete; otherwise it waits until both local days have ended
// (the later day-end), then each missing side needs its own personal protection.
func evaluatePair(st runState, low, high sideDays, since time.Time) (runState, []settledDay) {
	var out []settledDay
	for range 4000 {
		if st.Days == 0 {
			ld, hd, ok := findStart(st, low, high, since)
			if !ok {
				return st, out
			}
			st.Run++
			st.Days, st.Longest = 1, max(st.Longest, 1)
			st.StartLow, st.StartHigh, st.CursorLow, st.CursorHigh = ld, hd, ld, hd
			out = append(out, settledDay{Low: ld, High: hd, Outcome: "extended", Length: 1, Run: st.Run, Started: true, RunStartLow: ld, RunStartHigh: hd})
			continue
		}
		nl, nh := shiftDay(st.CursorLow, 1), shiftDay(st.CursorHigh, 1)
		la, ha := low.Active[nl], high.Active[nh]
		day := settledDay{Low: nl, High: nh, Run: st.Run, RunStartLow: st.StartLow, RunStartHigh: st.StartHigh}
		switch {
		case la && ha:
			st.Days++
			st.Longest = max(st.Longest, st.Days)
			day.Outcome, day.Length = "extended", st.Days
		case low.Decided < nl || high.Decided < nh:
			return st, out
		case (la || low.Protected[nl]) && (ha || high.Protected[nh]):
			day.Outcome, day.Length = "protected", st.Days
		default:
			day.Outcome, day.Length = "broken", st.Days
			st.Days = 0
		}
		st.CursorLow, st.CursorHigh = nl, nh
		out = append(out, day)
	}
	return st, out
}

// friendStreakView builds the viewer's picture: each friend's own local date and status,
// "checking" until the later day-end, never finalised on the viewer's own midnight.
func friendStreakView(st runState, you, friend sideDays, youIsLow bool, now time.Time) FriendStreakView {
	cy, cf := st.CursorLow, st.CursorHigh
	if !youIsLow {
		cy, cf = cf, cy
	}
	v := FriendStreakView{Days: st.Days, Longest: st.Longest}
	type side struct {
		out     *DaySide
		d       sideDays
		cursor  string
		shown   string
		settled bool
		done    bool
		prot    bool
		decided bool
	}
	sides := []*side{{out: &v.You, d: you, cursor: cy}, {out: &v.Friend, d: friend, cursor: cf}}
	for _, s := range sides {
		today := ""
		if loc, err := time.LoadLocation(s.d.Zone); err == nil {
			today = now.In(loc).Format(time.DateOnly)
		}
		s.shown = today
		if st.Days > 0 {
			s.shown = shiftDay(s.cursor, 1)
			if s.cursor == today {
				s.shown = s.cursor
			}
		}
		s.settled = st.Days > 0 && s.shown == s.cursor
		s.done = s.d.Active[s.shown]
		s.prot = !s.done && s.d.Protected[s.shown]
		s.decided = s.d.Decided >= s.shown
		*s.out = DaySide{LocalDate: s.shown, TimeZone: s.d.Zone, Done: s.done, Protected: s.prot}
	}
	a, b := sides[0], sides[1]
	switch {
	case st.Days == 0 && st.Longest > 0 && st.Run > 0:
		v.State = stateBroken
	case st.Days == 0:
		v.State = stateNone
	case a.settled && b.settled:
		v.State = stateBothDone
		if a.prot || b.prot {
			v.State = stateProtected
		}
	case a.done && b.done:
		v.State = stateBothDone
	case (!a.done && a.decided && !a.settled) || (!b.done && b.decided && !b.settled):
		v.State = stateChecking
		ready := (a.done || a.decided) && (b.done || b.decided)
		covered := (a.done || a.prot) && (b.done || b.prot)
		if ready && covered {
			v.State = stateProtected
		}
		if v.State == stateChecking {
			var until time.Time
			for _, s := range sides {
				if !s.done && !s.decided {
					if _, end, err := localDayBounds(s.shown, s.d.Zone); err == nil && end.After(until) {
						until = end
					}
				}
			}
			if !until.IsZero() {
				v.CheckingUntil = &until
			}
		}
	case b.done:
		v.State = stateWaitingYou
	case a.done:
		v.State = stateWaitingFriend
	default:
		v.State = stateWaitingBoth
	}
	return v
}

// streakEvent is an analytics notification produced inside a transaction and sent after commit.
type streakEvent struct {
	learner, name string
	props         map[string]any
}

func (s *Service) emitAll(events []streakEvent) {
	for _, e := range events {
		s.emit(e.learner, e.name, e.props)
	}
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

type pairQuery interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func nullDate(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// loadSide reads a learner's E5 projection from the cursor (or shortly before the
// friendship began) onward, so settled history is never reloaded.
func loadSide(ctx context.Context, q pairQuery, learner, from string) (sideDays, error) {
	side := sideDays{Active: map[string]bool{}, Protected: map[string]bool{}, Decided: "0001-01-01"}
	var decided *string
	err := q.QueryRow(ctx, `SELECT COALESCE(NULLIF(p.time_zone,''),'UTC'),(SELECT decided_through::text FROM streak_state WHERE learner_id=$1::uuid)
 FROM learners l LEFT JOIN learner_profiles p ON p.learner_id=l.id WHERE l.id=$1::uuid`, learner).Scan(&side.Zone, &decided)
	if err != nil {
		return side, err
	}
	if decided != nil {
		side.Decided = *decided
	}
	for _, table := range []struct {
		sql string
		dst map[string]bool
	}{
		{`SELECT local_date::text FROM streak_days WHERE learner_id=$1::uuid AND local_date>=$2::date`, side.Active},
		{`SELECT protected_on::text FROM streak_day_protections WHERE learner_id=$1::uuid AND protected_on>=$2::date`, side.Protected},
	} {
		rows, err := q.Query(ctx, table.sql, learner, from)
		if err != nil {
			return side, err
		}
		for rows.Next() {
			var d string
			if err = rows.Scan(&d); err != nil {
				rows.Close()
				return side, err
			}
			table.dst[d] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return side, err
		}
	}
	return side, nil
}

func lowerBound(cursor string, since time.Time) string {
	if cursor != "" {
		return cursor
	}
	return since.UTC().AddDate(0, 0, -2).Format(time.DateOnly)
}

// loadPairState reads the persisted run state of a pair (zero value when none).
func loadPairState(ctx context.Context, q pairQuery, lo, hi string) (runState, error) {
	var st runState
	var cl, ch, sl, sh *string
	err := q.QueryRow(ctx, `SELECT current_days,longest_days,run_no,cursor_low::text,cursor_high::text,run_start_low::text,run_start_high::text FROM friend_streaks WHERE learner_low=$1::uuid AND learner_high=$2::uuid`, lo, hi).
		Scan(&st.Days, &st.Longest, &st.Run, &cl, &ch, &sl, &sh)
	if errors.Is(err, pgx.ErrNoRows) {
		return runState{}, nil
	}
	st.CursorLow, st.CursorHigh, st.StartLow, st.StartHigh = nullDate(cl), nullDate(ch), nullDate(sl), nullDate(sh)
	return st, err
}

// EvaluateFriendStreaks settles every Friend streak of the learner. It is idempotent and
// safe to run concurrently: each pair is settled under the pair's learner locks.
func (s *Service) EvaluateFriendStreaks(ctx context.Context, learnerID string) error {
	rows, err := s.pool.Query(ctx, `SELECT learner_low::text,learner_high::text FROM friendships WHERE learner_low=$1::uuid OR learner_high=$1::uuid ORDER BY created_at,learner_low,learner_high LIMIT 1000`, learnerID)
	if err != nil {
		return err
	}
	type pairIDs struct{ lo, hi string }
	pairs := []pairIDs{}
	for rows.Next() {
		var p pairIDs
		if err = rows.Scan(&p.lo, &p.hi); err != nil {
			rows.Close()
			return err
		}
		pairs = append(pairs, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var result error
	for _, p := range pairs {
		result = errors.Join(result, s.settlePair(ctx, p.lo, p.hi))
	}
	return result
}

func (s *Service) settlePair(ctx context.Context, lo, hi string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockLearners(ctx, tx, lo, hi); err != nil {
		return err
	}
	var since time.Time
	err = tx.QueryRow(ctx, `SELECT created_at FROM friendships WHERE learner_low=$1::uuid AND learner_high=$2::uuid`, lo, hi).Scan(&since)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // the friendship ended meanwhile
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO friend_streaks(learner_low,learner_high) VALUES($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, lo, hi); err != nil {
		return err
	}
	st, err := loadPairState(ctx, tx, lo, hi)
	if err != nil {
		return err
	}
	low, err := loadSide(ctx, tx, lo, lowerBound(st.CursorLow, since))
	if err != nil {
		return err
	}
	high, err := loadSide(ctx, tx, hi, lowerBound(st.CursorHigh, since))
	if err != nil {
		return err
	}
	next, settled := evaluatePair(st, low, high, since)
	var milestoneDays, freezeCap int
	if err = tx.QueryRow(ctx, `SELECT friend_streak_milestone_days,freeze_cap FROM gamification_config WHERE id`).Scan(&milestoneDays, &freezeCap); err != nil {
		return err
	}
	events := []streakEvent{}
	for _, day := range settled {
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO friend_streak_days(learner_low,learner_high,low_date,high_date,run_no,outcome,length) VALUES($1::uuid,$2::uuid,$3::date,$4::date,$5,$6,$7)
 ON CONFLICT DO NOTHING RETURNING id::text`, lo, hi, day.Low, day.High, day.Run, day.Outcome, day.Length).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // already settled by an earlier run
		}
		if err != nil {
			return err
		}
		both := func(name string, props func(date string) map[string]any) {
			events = append(events,
				streakEvent{lo, name, props(day.Low)},
				streakEvent{hi, name, props(day.High)})
		}
		switch day.Outcome {
		case "extended":
			name := "friend_streak_extended"
			if day.Started {
				name = "friend_streak_started"
			}
			both(name, func(date string) map[string]any {
				return map[string]any{"event_id": id, "local_date": date, "streak_length": day.Length}
			})
			if milestoneDays > 0 && day.Length%milestoneDays == 0 {
				grants, err := grantMilestone(ctx, tx, lo, hi, day, freezeCap)
				if err != nil {
					return err
				}
				events = append(events, grants...)
			}
		case "broken":
			both("friend_streak_broken", func(string) map[string]any {
				return map[string]any{"event_id": id, "previous_length": day.Length, "reason": "missed"}
			})
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE friend_streaks SET current_days=$3,longest_days=$4,run_no=$5,cursor_low=NULLIF($6,'')::date,cursor_high=NULLIF($7,'')::date,
 run_start_low=NULLIF($8,'')::date,run_start_high=NULLIF($9,'')::date,updated_at=now() WHERE learner_low=$1::uuid AND learner_high=$2::uuid`,
		lo, hi, next.Days, next.Longest, next.Run, next.CursorLow, next.CursorHigh, next.StartLow, next.StartHigh); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.emitAll(events)
	return nil
}

// grantMilestone records the pair milestone once and grants each learner one freeze below
// the E5 cap. The learner key (own run start and milestone) also dedupes across friends.
func grantMilestone(ctx context.Context, tx pgx.Tx, lo, hi string, day settledDay, freezeCap int) ([]streakEvent, error) {
	var run int
	err := tx.QueryRow(ctx, `INSERT INTO friend_streak_milestones(learner_low,learner_high,run_no,milestone_days,reached_low,reached_high) VALUES($1::uuid,$2::uuid,$3,$4,$5::date,$6::date)
 ON CONFLICT DO NOTHING RETURNING run_no`, lo, hi, day.Run, day.Length, day.Low, day.High).Scan(&run)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	events := []streakEvent{}
	for _, side := range []struct{ learner, reached, started string }{{lo, day.Low, day.RunStartLow}, {hi, day.High, day.RunStartHigh}} {
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO streak_freeze_milestones(learner_id,kind,milestone_days,run_started_on,reached_on,granted)
 SELECT $1::uuid,'friend_streak',$2,$3::date,$4::date,
  ((SELECT count(*) FROM streak_freeze_milestones WHERE learner_id=$1::uuid AND granted)-(SELECT count(*) FROM streak_day_protections WHERE learner_id=$1::uuid))<$5
 ON CONFLICT(learner_id,kind,milestone_days,run_started_on) DO NOTHING RETURNING id::text`, side.learner, day.Length, side.started, side.reached, freezeCap).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var granted bool
		if err = tx.QueryRow(ctx, `SELECT granted FROM streak_freeze_milestones WHERE id=$1::uuid`, id).Scan(&granted); err != nil {
			return nil, err
		}
		if err = gamification.RefreshInventory(ctx, tx, side.learner); err != nil {
			return nil, err
		}
		if granted {
			events = append(events, streakEvent{side.learner, "streak_freeze_earned", map[string]any{"event_id": id, "reason": "friend_streak", "streak_length": day.Length, "local_date": side.reached}})
		}
	}
	return events, nil
}

// endFriendStreakTx reports the broken-by-ending event for a live pair state. The pair
// rows themselves disappear with the friendship (ON DELETE CASCADE).
func endFriendStreakTx(ctx context.Context, tx pgx.Tx, lo, hi string) ([]streakEvent, error) {
	var days int
	var id string
	err := tx.QueryRow(ctx, `SELECT current_days,gen_random_uuid()::text FROM friend_streaks WHERE learner_low=$1::uuid AND learner_high=$2::uuid`, lo, hi).Scan(&days, &id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && days == 0) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	props := func() map[string]any {
		return map[string]any{"event_id": id, "previous_length": days, "reason": "friendship_ended"}
	}
	return []streakEvent{{lo, "friend_streak_broken", props()}, {hi, "friend_streak_broken", props()}}, nil
}

// SweepFriendStreaks re-settles live pairs whose last check is stale, so a day-end is
// decided even when neither friend opens the app. Bounded and idempotent.
func (s *Service) SweepFriendStreaks(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT learner_low::text,learner_high::text FROM friend_streaks WHERE current_days>0 AND updated_at<now()-interval '1 minute' ORDER BY updated_at LIMIT 100`)
	if err != nil {
		return err
	}
	type pairIDs struct{ lo, hi string }
	pairs := []pairIDs{}
	for rows.Next() {
		var p pairIDs
		if err = rows.Scan(&p.lo, &p.hi); err != nil {
			rows.Close()
			return err
		}
		pairs = append(pairs, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var result error
	for _, p := range pairs {
		result = errors.Join(result, s.settlePair(ctx, p.lo, p.hi))
	}
	return result
}

// Maintain runs the sweep until the context ends.
func (s *Service) Maintain(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.SweepFriendStreaks(ctx); err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "friend streak sweep failed", "error", fmt.Sprint(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
