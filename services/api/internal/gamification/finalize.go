package gamification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// leagueCloseGrace is how long after Sunday ends in UTC a week stays open: UTC-12 is the last
// zone to reach Monday, so after this no Learner can still be on Sunday.
const leagueCloseGrace = 12 * time.Hour

// LeagueClosed reports whether every time zone has left the week that starts on weekStart.
func LeagueClosed(weekStart string, now time.Time) bool {
	day, err := time.Parse(time.DateOnly, weekStart)
	if err != nil {
		return false
	}
	return !now.Before(day.AddDate(0, 0, 7).Add(leagueCloseGrace))
}

// LeagueResult is one member's final standing and outcome.
type LeagueResult struct {
	Learner          string
	XP               int64
	Slot, Rank       int
	Tie              bool
	Outcome          string // promoted, demoted or stayed
	FromTier, ToTier int
	Members          int
	ID, WeekStart    string
}

// FinalizeLeague ranks a closed group and decides every outcome from the configured zones.
// Ties keep the earlier slot (the ranking order), so a boundary tie is never random. A member
// with no XP is never promoted, and groups too small for a zone keep everyone in place.
func FinalizeLeague(rules LeagueRules, tier int, entries []LeagueEntry) []LeagueResult {
	ranked := RankLeague(entries)
	out := make([]LeagueResult, len(ranked))
	for i, e := range ranked {
		r := LeagueResult{Learner: e.Learner, XP: e.XP, Slot: e.Slot, Rank: e.Rank, Tie: e.Tie, Outcome: "stayed", FromTier: tier, ToTier: tier, Members: len(ranked)}
		switch rules.Zone(tier, len(ranked), e.Rank) {
		case "promotion":
			if e.XP > 0 {
				r.Outcome, r.ToTier = "promoted", min(tier+1, len(rules.Tiers))
			}
		case "demotion":
			r.Outcome, r.ToTier = "demoted", max(tier-1, 1)
		}
		out[i] = r
	}
	return out
}

type ownedEvent struct {
	owner, name string
	props       map[string]any
}

// leagueEvents builds the promotion and demotion analytics; staying emits nothing.
func leagueEvents(league string, results []LeagueResult) []ownedEvent {
	var events []ownedEvent
	for _, r := range results {
		if r.Outcome == "stayed" {
			continue
		}
		events = append(events, ownedEvent{r.Learner, "league_" + r.Outcome, map[string]any{
			"event_id": r.ID, "week_start": r.WeekStart, "league": league, "rank": r.Rank, "from_tier": r.FromTier, "to_tier": r.ToTier,
		}})
	}
	return events
}

// FinalizeLeagues closes every ended league group once. Each group commits in one transaction guarded
// by its row lock and finalized_at, so retries, concurrent workers and restarts never double-apply.
func (s *Service) FinalizeLeagues(ctx context.Context) error {
	now := s.now()
	rows, err := s.pool.Query(ctx, `SELECT id::text FROM league_groups
 WHERE finalized_at IS NULL AND week_start+7 <= (($1::timestamptz - interval '12 hours') AT TIME ZONE 'UTC')::date
 ORDER BY week_start,id LIMIT 100`, now)
	if err != nil {
		return err
	}
	var groups []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var result error
	for _, group := range groups {
		result = errors.Join(result, s.finalizeGroup(ctx, group, now))
	}
	return result
}

func (s *Service) finalizeGroup(ctx context.Context, group string, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var week string
	var tier int
	err = tx.QueryRow(ctx, `SELECT week_start::text,tier FROM league_groups WHERE id=$1::uuid AND finalized_at IS NULL FOR UPDATE SKIP LOCKED`, group).Scan(&week, &tier)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // finalized, or being finalized by another worker
	}
	if err != nil {
		return err
	}
	rules, err := loadLeagueRules(ctx, tx)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT m.learner_id::text,m.slot,COALESCE(sum(x.amount),0)::bigint
 FROM league_memberships m LEFT JOIN xp_events x ON x.learner_id=m.learner_id AND x.local_date BETWEEN m.week_start AND m.week_start+6
 WHERE m.group_id=$1::uuid GROUP BY m.learner_id,m.slot`, group)
	if err != nil {
		return err
	}
	entries := []LeagueEntry{}
	for rows.Next() {
		var e LeagueEntry
		if err = rows.Scan(&e.Learner, &e.Slot, &e.XP); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	results := FinalizeLeague(rules, tier, entries)
	fresh := results[:0:0]
	for _, r := range results {
		err = tx.QueryRow(ctx, `INSERT INTO league_week_results(id,learner_id,week_start,group_id,tier_from,tier_to,final_rank,members,weekly_xp,outcome,finalized_at)
 VALUES(md5($1::text||'|'||$2::text)::uuid,$1::uuid,$2::date,$3::uuid,$4,$5,$6,$7,$8,$9,$10)
 ON CONFLICT(learner_id,week_start) DO NOTHING RETURNING id::text`, r.Learner, week, group, r.FromTier, r.ToTier, r.Rank, r.Members, r.XP, r.Outcome, now).Scan(&r.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // a restored result already stands; never decide it twice
		}
		if err != nil {
			return err
		}
		r.WeekStart = week
		fresh = append(fresh, r)
	}
	stale, err := s.correctNextWeek(ctx, tx, week, results)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE league_groups SET finalized_at=$2 WHERE id=$1::uuid`, group, now); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if s.rdb != nil && len(stale) > 0 {
		keys := make([]string, len(stale))
		for i, g := range stale {
			keys[i] = leagueKey(g)
		}
		if err = s.rdb.Del(ctx, keys...).Err(); err != nil {
			slog.WarnContext(ctx, "league cache invalidation skipped", "error", err)
		}
	}
	name := rules.Tiers[min(max(tier, 1), len(rules.Tiers))-1]
	for _, e := range leagueEvents(name, fresh) {
		s.emit(ctx, e.owner, []pendingEvent{{e.name, e.props}})
	}
	return nil
}

// correctNextWeek moves Learners who started the next week before this one closed into the tier
// their result earned. The old slot stays burned, as for any membership removal.
func (s *Service) correctNextWeek(ctx context.Context, tx pgx.Tx, week string, results []LeagueResult) ([]string, error) {
	next, err := addDays(week, 7)
	if err != nil {
		return nil, err
	}
	want := map[string]int{}
	ids := make([]string, 0, len(results))
	for _, r := range results {
		want[r.Learner] = r.ToTier
		ids = append(ids, r.Learner)
	}
	rows, err := tx.Query(ctx, `SELECT learner_id::text,tier,group_id::text FROM league_memberships WHERE week_start=$1::date AND learner_id=ANY($2::uuid[])`, next, ids)
	if err != nil {
		return nil, err
	}
	type move struct{ learner, group string }
	var moves []move
	for rows.Next() {
		var id, group string
		var tier int
		if err = rows.Scan(&id, &tier, &group); err != nil {
			rows.Close()
			return nil, err
		}
		if tier != want[id] {
			moves = append(moves, move{id, group})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(moves) == 0 {
		return nil, err
	}
	// Same lock order as a join: Learner rows (sorted) first, then the tier allocator.
	slices.SortFunc(moves, func(a, b move) int { return strings.Compare(a.learner, b.learner) })
	var stale []string
	for _, m := range moves {
		var locked string
		if err = tx.QueryRow(ctx, `SELECT id::text FROM learners WHERE id=$1::uuid FOR UPDATE`, m.learner).Scan(&locked); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM league_memberships WHERE learner_id=$1::uuid AND week_start=$2::date`, m.learner, next); err != nil {
			return nil, err
		}
		if _, err = s.joinLeague(ctx, tx, m.learner, next); err != nil {
			return nil, fmt.Errorf("correct league tier: %w", err)
		}
		stale = append(stale, m.group)
	}
	return stale, nil
}

// LastWeek is the prior week's outcome: final, still closing, or none (no membership).
type LastWeek struct {
	State      string       `json:"state"`
	Week       LastWeekSpan `json:"week"`
	League     string       `json:"league,omitempty"`
	Tier       int          `json:"tier,omitempty"`
	Rank       int          `json:"rank,omitempty"`
	Members    int          `json:"members,omitempty"`
	WeeklyXP   int64        `json:"weekly_xp,omitempty"`
	Outcome    string       `json:"outcome,omitempty"`
	NextLeague string       `json:"next_league,omitempty"`
}

// LastWeekSpan is the prior league week.
type LastWeekSpan struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func (s *Service) lastWeek(ctx context.Context, owner, week string) (*LastWeek, error) {
	prev, err := addDays(week, -7)
	if err != nil {
		return nil, err
	}
	end, _ := WeekEnd(prev)
	lw := &LastWeek{State: "none", Week: LastWeekSpan{Start: prev, End: end}}
	var from, to int
	err = s.pool.QueryRow(ctx, `SELECT tier_from,tier_to,final_rank,members,weekly_xp,outcome FROM league_week_results WHERE learner_id=$1::uuid AND week_start=$2::date`, owner, prev).
		Scan(&from, &to, &lw.Rank, &lw.Members, &lw.WeeklyXP, &lw.Outcome)
	switch {
	case err == nil:
		rules, rerr := loadLeagueRules(ctx, s.pool)
		if rerr != nil {
			return nil, rerr
		}
		name := func(t int) string { return rules.Tiers[min(max(t, 1), len(rules.Tiers))-1] }
		lw.State, lw.Tier, lw.League, lw.NextLeague = "final", from, name(from), name(to)
	case errors.Is(err, pgx.ErrNoRows):
		var member bool
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM league_memberships WHERE learner_id=$1::uuid AND week_start=$2::date)`, owner, prev).Scan(&member); err != nil {
			return nil, err
		}
		if member {
			lw.State = "closing"
		}
	default:
		return nil, err
	}
	return lw, nil
}
