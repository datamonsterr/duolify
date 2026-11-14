package gamification

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// FreezeRules are the configured effort milestone and inventory cap.
type FreezeRules struct{ MilestoneDays, Cap int }

// Validate rejects a rule set the database would also reject.
func (r FreezeRules) Validate() error {
	if r.MilestoneDays < 1 || r.Cap < 0 {
		return errors.New("invalid streak freeze config")
	}
	return nil
}

// Protection is one missed day covered by an automatically consumed freeze.
type Protection struct {
	Date                    string
	Remaining, StreakLength int
}

// Grant is one freeze milestone. Granted is false when the cap was already full.
type Grant struct {
	Days                    int
	RunStartedOn, ReachedOn string
	Granted                 bool
}

// StreakOutcome extends the streak result with the freeze decisions it implies.
type StreakOutcome struct {
	StreakResult
	Protections []Protection
	Grants      []Grant
	Held        int
	RunLength   map[string]int // streak length reached on each active date
}

// SimulateStreak walks ascending distinct local dates. Active days extend a run and
// earn a freeze at every configured milestone while below the cap. Each judged missed
// day (<= decided) of a live run consumes one freeze, which preserves the run without
// incrementing it, or breaks the run when none is held. Days after decided are never judged.
func SimulateStreak(dates []string, decided string, rules FreezeRules) StreakOutcome {
	return SimulateStreakWith(dates, decided, rules, nil)
}

// SimulateStreakWith also adds one freeze on each date of extra: the recorded, already
// cap-checked Friend streak milestone grants (E6-06), so one inventory serves both.
func SimulateStreakWith(dates []string, decided string, rules FreezeRules, extraGrants []string) StreakOutcome {
	o := StreakOutcome{RunLength: map[string]int{}}
	run, held := 0, 0
	prev, runStart := "", ""
	alive := false
	judge := func(from, before string) {
		for day := from; alive && day <= decided && (before == "" || day < before); day, _ = addDays(day, 1) {
			if held > 0 {
				held--
				o.Protections = append(o.Protections, Protection{day, held, run})
				continue
			}
			o.Breaks = append(o.Breaks, StreakBreak{BrokenOn: day, PreviousLength: run})
			run, alive = 0, false
		}
	}
	extra := map[string]int{}
	for _, d := range extraGrants {
		extra[d]++
	}
	for _, date := range dates {
		if prev != "" {
			next, _ := addDays(prev, 1)
			judge(next, date)
		}
		if !alive {
			run, runStart, alive = 0, date, true
		}
		run++
		o.Longest = max(o.Longest, run)
		o.RunLength[date] = run
		if rules.MilestoneDays > 0 && run%rules.MilestoneDays == 0 {
			grant := Grant{Days: run, RunStartedOn: runStart, ReachedOn: date, Granted: held < rules.Cap}
			if grant.Granted {
				held++
			}
			o.Grants = append(o.Grants, grant)
		}
		held += extra[date] // recorded Friend streak milestone freezes (already cap-checked)
		prev = date
	}
	if prev != "" {
		o.LastActive = prev
		next, _ := addDays(prev, 1)
		judge(next, "")
	}
	o.Current, o.Held = run, held
	return o
}

func loadFreezeRules(ctx context.Context, tx pgx.Tx) (FreezeRules, error) {
	var r FreezeRules
	if err := tx.QueryRow(ctx, `SELECT freeze_milestone_days,freeze_cap FROM gamification_config WHERE id`).Scan(&r.MilestoneDays, &r.Cap); err != nil {
		return r, err
	}
	return r, r.Validate()
}

// applyFreezes persists the simulation's milestones and day protections. Database
// uniqueness makes replay and concurrent workers no-ops; the inventory is derived from
// the recorded rows under the learner lock so it cannot drift or go negative.
func applyFreezes(ctx context.Context, tx pgx.Tx, owner string, o StreakOutcome) ([]pendingEvent, error) {
	events := []pendingEvent{}
	for _, g := range o.Grants {
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO streak_freeze_milestones(learner_id,kind,milestone_days,run_started_on,reached_on,granted)
 VALUES($1::uuid,'streak',$2,$3::date,$4::date,$5) ON CONFLICT(learner_id,kind,milestone_days,run_started_on) DO NOTHING RETURNING id::text`,
			owner, g.Days, g.RunStartedOn, g.ReachedOn, g.Granted).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if g.Granted {
			events = append(events, pendingEvent{"streak_freeze_earned", map[string]any{"event_id": id, "reason": "streak", "streak_length": g.Days, "local_date": g.ReachedOn}})
		}
	}
	for _, p := range o.Protections {
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO streak_day_protections(learner_id,source,protected_on,remaining,streak_length)
 VALUES($1::uuid,'streak',$2::date,$3,$4) ON CONFLICT(learner_id,protected_on) DO NOTHING RETURNING id::text`,
			owner, p.Date, p.Remaining, p.StreakLength).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		events = append(events, pendingEvent{"streak_freeze_used", map[string]any{"event_id": id, "protected_on": p.Date, "remaining": p.Remaining, "streak_length": p.StreakLength}})
	}
	return events, RefreshInventory(ctx, tx, owner)
}

// RefreshInventory derives the single freeze inventory from the recorded grants (personal
// and Friend streak) minus the recorded protections. Callers hold the learner lock.
func RefreshInventory(ctx context.Context, tx pgx.Tx, owner string) error {
	_, err := tx.Exec(ctx, `INSERT INTO streak_freeze_inventory(learner_id,held,updated_at)
 SELECT $1::uuid,GREATEST(0,(SELECT count(*) FROM streak_freeze_milestones WHERE learner_id=$1::uuid AND granted)-(SELECT count(*) FROM streak_day_protections WHERE learner_id=$1::uuid)),now()
 ON CONFLICT(learner_id) DO UPDATE SET held=EXCLUDED.held,updated_at=EXCLUDED.updated_at`, owner)
	return err
}
