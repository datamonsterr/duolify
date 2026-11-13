package gamification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

// leagueCacheTTL bounds how long a cached ranking may lag Postgres.
const leagueCacheTTL = time.Minute

// LeagueRules is the configured league shape.
type LeagueRules struct {
	Tiers                               []string
	GroupSize, PromoteTop, DemoteBottom int
}

// Validate rejects ambiguous league configuration.
func (r LeagueRules) Validate() error {
	if len(r.Tiers) < 1 || r.GroupSize < 2 || r.GroupSize > 999 || r.PromoteTop < 0 || r.DemoteBottom < 0 || r.PromoteTop+r.DemoteBottom > r.GroupSize {
		return errors.New("invalid league config")
	}
	return nil
}

func (r LeagueRules) zones(members int) (promote, demote int) {
	return min(r.PromoteTop, members/2), min(r.DemoteBottom, members/2)
}

// PromotionTop is the number of ranks in the promotion zone (0 in the top tier).
func (r LeagueRules) PromotionTop(tier, members int) int {
	if tier >= len(r.Tiers) {
		return 0
	}
	promote, _ := r.zones(members)
	return promote
}

// DemotionFrom is the first rank of the demotion zone (0 when absent, as in the lowest tier).
func (r LeagueRules) DemotionFrom(tier, members int) int {
	_, demote := r.zones(members)
	if tier <= 1 || demote == 0 {
		return 0
	}
	return members - demote + 1
}

// Zone names the text zone of a rank: promotion, demotion or none. Zones are halves at most, so they never overlap.
func (r LeagueRules) Zone(tier, members, rank int) string {
	if top := r.PromotionTop(tier, members); rank >= 1 && rank <= top {
		return "promotion"
	}
	if from := r.DemotionFrom(tier, members); from > 0 && rank >= from {
		return "demotion"
	}
	return "none"
}

// WeekStart returns the Monday of the league week containing a local date.
func WeekStart(date string) (string, error) {
	day, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return "", fmt.Errorf("invalid date %q", date)
	}
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7)).Format(time.DateOnly), nil
}

// WeekEnd returns the Sunday closing the league week that starts on a Monday.
func WeekEnd(start string) (string, error) { return addDays(start, 6) }

// LeagueScore orders by XP descending then earlier slot, exactly in float64.
func LeagueScore(xp int64, slot int) float64 { return float64(xp*1000 + int64(999-slot)) }

// SplitLeagueScore reverses LeagueScore.
func SplitLeagueScore(score float64) (xp int64, slot int) {
	v := int64(score)
	return v / 1000, 999 - int(v%1000)
}

// LeagueEntry is one member's weekly XP and join slot.
type LeagueEntry struct {
	Learner string
	XP      int64
	Slot    int
}

// RankedEntry adds the deterministic rank and tie flag.
type RankedEntry struct {
	LeagueEntry
	Rank int
	Tie  bool
}

// RankLeague orders by weekly XP descending, then earlier slot; equal XP is flagged as a tie.
func RankLeague(entries []LeagueEntry) []RankedEntry {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b LeagueEntry) int {
		if a.XP != b.XP {
			return int(b.XP - a.XP)
		}
		return a.Slot - b.Slot
	})
	ranked := make([]RankedEntry, len(sorted))
	for i, e := range sorted {
		ranked[i] = RankedEntry{LeagueEntry: e, Rank: i + 1, Tie: (i > 0 && sorted[i-1].XP == e.XP) || (i+1 < len(sorted) && sorted[i+1].XP == e.XP)}
	}
	return ranked
}

// PageLeague slices a ranking and returns the next and previous offsets.
func PageLeague(ranked []RankedEntry, offset, limit int) (page []RankedEntry, next, prev *int) {
	if offset > len(ranked) {
		offset = len(ranked)
	}
	end := min(offset+limit, len(ranked))
	page = ranked[offset:end]
	if end < len(ranked) {
		next = &end
	}
	if offset > 0 {
		p := max(0, offset-limit)
		prev = &p
	}
	return page, next, prev
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadLeagueRules(ctx context.Context, q queryer) (LeagueRules, error) {
	var r LeagueRules
	err := q.QueryRow(ctx, `SELECT league_tiers,league_group_size,league_promote_top,league_demote_bottom FROM gamification_config WHERE id`).Scan(&r.Tiers, &r.GroupSize, &r.PromoteTop, &r.DemoteBottom)
	if err != nil {
		return r, err
	}
	return r, r.Validate()
}

// UseCache attaches the disposable Redis ranking cache; nil serves from Postgres only.
func (s *Service) UseCache(c *redis.Client) { s.rdb = c }

type leagueSlot struct{ group, week string }

// joinLeague assigns the learner to their week's group inside the caller's transaction (learner row locked).
func (s *Service) joinLeague(ctx context.Context, tx pgx.Tx, owner, localDate string) (leagueSlot, error) {
	week, err := WeekStart(localDate)
	if err != nil {
		return leagueSlot{}, err
	}
	var group string
	err = tx.QueryRow(ctx, `SELECT group_id::text FROM league_memberships WHERE learner_id=$1::uuid AND week_start=$2::date`, owner, week).Scan(&group)
	if err == nil {
		return leagueSlot{group, week}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return leagueSlot{}, err
	}
	rules, err := loadLeagueRules(ctx, tx)
	if err != nil {
		return leagueSlot{}, err
	}
	tier := 1
	// A finalized result decides the next tier; an unfinalized week is corrected when it closes.
	err = tx.QueryRow(ctx, `SELECT COALESCE(r.tier_to,m.tier) FROM league_memberships m LEFT JOIN league_week_results r ON r.learner_id=m.learner_id AND r.week_start=m.week_start
 WHERE m.learner_id=$1::uuid AND m.week_start<$2::date ORDER BY m.week_start DESC LIMIT 1`, owner, week).Scan(&tier)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return leagueSlot{}, err
	}
	tier = min(max(tier, 1), len(rules.Tiers))
	// One allocator per week and tier keeps groups from overfilling or duplicating.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("league:%s:%d", week, tier)); err != nil {
		return leagueSlot{}, err
	}
	err = tx.QueryRow(ctx, `SELECT id::text FROM league_groups WHERE week_start=$1::date AND tier=$2 AND size<$3 AND finalized_at IS NULL ORDER BY created_at,id LIMIT 1 FOR UPDATE`, week, tier, rules.GroupSize).Scan(&group)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO league_groups(week_start,tier) VALUES($1::date,$2) RETURNING id::text`, week, tier).Scan(&group)
	}
	if err != nil {
		return leagueSlot{}, err
	}
	var slot int
	if err = tx.QueryRow(ctx, `UPDATE league_groups SET size=size+1 WHERE id=$1::uuid RETURNING size`, group).Scan(&slot); err != nil {
		return leagueSlot{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO league_memberships(learner_id,week_start,group_id,tier,slot) VALUES($1::uuid,$2::date,$3::uuid,$4,$5)`, owner, week, group, tier, slot); err != nil {
		return leagueSlot{}, err
	}
	return leagueSlot{group, week}, nil
}

func leagueKey(group string) string { return "league:" + group }

// refreshLeague raises a member's cached score after commit. A missing key is rebuilt by the next read.
func (s *Service) refreshLeague(ctx context.Context, owner string, slots []leagueSlot) {
	if s.rdb == nil {
		return
	}
	for _, l := range slots {
		var xp int64
		var slot int
		err := s.pool.QueryRow(ctx, `SELECT m.slot,COALESCE((SELECT sum(amount) FROM xp_events x WHERE x.learner_id=m.learner_id AND x.local_date BETWEEN m.week_start AND m.week_start+6),0)
 FROM league_memberships m WHERE m.learner_id=$1::uuid AND m.week_start=$2::date`, owner, l.week).Scan(&slot, &xp)
		if err == nil {
			if exists, rerr := s.rdb.Exists(ctx, leagueKey(l.group)).Result(); rerr != nil || exists == 0 {
				err = rerr
			} else {
				err = s.rdb.ZAddArgs(ctx, leagueKey(l.group), redis.ZAddArgs{GT: true, Members: []redis.Z{{Score: LeagueScore(xp, slot), Member: owner}}}).Err()
			}
		}
		if err != nil {
			slog.WarnContext(ctx, "league cache refresh skipped", "error", err)
		}
	}
}

func (s *Service) leagueFromPostgres(ctx context.Context, group string) ([]LeagueEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT m.learner_id::text,m.slot,COALESCE(sum(x.amount),0)::bigint
 FROM league_memberships m LEFT JOIN xp_events x ON x.learner_id=m.learner_id AND x.local_date BETWEEN m.week_start AND m.week_start+6
 WHERE m.group_id=$1::uuid GROUP BY m.learner_id,m.slot`, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []LeagueEntry{}
	for rows.Next() {
		var e LeagueEntry
		if err = rows.Scan(&e.Learner, &e.Slot, &e.XP); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// leagueEntries serves the cached ranking when it matches the member count, else rebuilds it exactly from Postgres.
func (s *Service) leagueEntries(ctx context.Context, group string, members int) ([]LeagueEntry, error) {
	if s.rdb != nil {
		cached, err := s.rdb.ZRevRangeWithScores(ctx, leagueKey(group), 0, -1).Result()
		if err == nil && len(cached) == members {
			entries := make([]LeagueEntry, len(cached))
			for i, z := range cached {
				xp, slot := SplitLeagueScore(z.Score)
				entries[i] = LeagueEntry{Learner: z.Member.(string), XP: xp, Slot: slot}
			}
			return entries, nil
		}
		if err != nil {
			slog.WarnContext(ctx, "league cache unavailable", "error", err)
		}
	}
	entries, err := s.leagueFromPostgres(ctx, group)
	if err != nil || s.rdb == nil {
		return entries, err
	}
	if perr := s.cacheLeague(ctx, group, entries); perr != nil {
		slog.WarnContext(ctx, "league cache rebuild skipped", "error", perr)
	}
	return entries, nil
}

// cacheLeague replaces one group's disposable Redis ranking with the Postgres entries.
func (s *Service) cacheLeague(ctx context.Context, group string, entries []LeagueEntry) error {
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, leagueKey(group))
	if len(entries) > 0 {
		zs := make([]redis.Z, len(entries))
		for i, e := range entries {
			zs[i] = redis.Z{Score: LeagueScore(e.XP, e.Slot), Member: e.Learner}
		}
		pipe.ZAdd(ctx, leagueKey(group), zs...)
		pipe.Expire(ctx, leagueKey(group), leagueCacheTTL)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// RebuildLeagueCache repopulates every league ranking from Postgres after a restore
// or Redis loss and returns the number of groups rebuilt. Redis stays disposable.
func (s *Service) RebuildLeagueCache(ctx context.Context) (int, error) {
	if s.rdb == nil {
		return 0, errors.New("league cache is not configured")
	}
	rows, err := s.pool.Query(ctx, "SELECT DISTINCT group_id::text FROM league_memberships")
	if err != nil {
		return 0, err
	}
	var groups []string
	for rows.Next() {
		var group string
		if err = rows.Scan(&group); err != nil {
			rows.Close()
			return 0, err
		}
		groups = append(groups, group)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for _, group := range groups {
		entries, err := s.leagueFromPostgres(ctx, group)
		if err != nil {
			return 0, err
		}
		if err = s.cacheLeague(ctx, group, entries); err != nil {
			return 0, err
		}
	}
	return len(groups), nil
}

// LeagueView is the authenticated Learner's own weekly league page.
type LeagueView struct {
	State     string      `json:"state"`
	Week      LeagueWeek  `json:"week"`
	AsOf      string      `json:"as_of"`
	LocalDate string      `json:"local_date"`
	TimeZone  string      `json:"time_zone"`
	League    *LeagueInfo `json:"league,omitempty"`
	Self      *LeagueSelf `json:"self,omitempty"`
	Rows      []LeagueRow `json:"rows"`
	Page      LeaguePage  `json:"page"`
	LastWeek  *LastWeek   `json:"last_week,omitempty"`
}

// LeagueWeek is the Monday to Sunday league week in the Learner's local dates.
type LeagueWeek struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	DaysLeft int    `json:"days_left"`
}

// LeagueInfo names the tier and the configured text zones.
type LeagueInfo struct {
	Name         string `json:"name"`
	Tier         int    `json:"tier"`
	Members      int    `json:"members"`
	PromotionTop int    `json:"promotion_top"`
	DemotionFrom int    `json:"demotion_from"`
}

// LeagueSelf is the Learner's own rank.
type LeagueSelf struct {
	Rank     int    `json:"rank"`
	WeeklyXP int64  `json:"weekly_xp"`
	Zone     string `json:"zone"`
}

// LeagueRow is one ranked member; only display name and XP are shared.
type LeagueRow struct {
	Rank        int    `json:"rank"`
	DisplayName string `json:"display_name"`
	WeeklyXP    int64  `json:"weekly_xp"`
	Tie         bool   `json:"tie"`
	Self        bool   `json:"self"`
	Zone        string `json:"zone"`
}

// LeaguePage carries offsets for keyboard-friendly paging.
type LeaguePage struct {
	Offset     int  `json:"offset"`
	Limit      int  `json:"limit"`
	Total      int  `json:"total"`
	NextOffset *int `json:"next_offset"`
	PrevOffset *int `json:"prev_offset"`
}

// League reads the learner's current-week group. Reading never invents outcomes or groups for learners without XP.
func (s *Service) League(ctx context.Context, owner string, offset, limit int) (LeagueView, error) {
	view := LeagueView{Rows: []LeagueRow{}, Page: LeaguePage{Offset: offset, Limit: limit}}
	var zone string
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(NULLIF(time_zone,''),'UTC') FROM learner_profiles WHERE learner_id=$1::uuid`, owner).Scan(&zone); err != nil {
		return view, err
	}
	now := s.now()
	local, err := LocalDate(now, zone)
	if err != nil {
		return view, err
	}
	week, _ := WeekStart(local)
	end, _ := WeekEnd(week)
	endDay, _ := time.Parse(time.DateOnly, end)
	today, _ := time.Parse(time.DateOnly, local)
	view.State, view.AsOf, view.LocalDate, view.TimeZone = "not_joined", now.UTC().Format(time.RFC3339), local, zone
	view.Week = LeagueWeek{Start: week, End: end, DaysLeft: int(endDay.Sub(today).Hours()/24) + 1}

	if view.LastWeek, err = s.lastWeek(ctx, owner, week); err != nil {
		return view, err
	}
	var group string
	var tier, members int
	lookup := func() error {
		return s.pool.QueryRow(ctx, `SELECT m.group_id::text,m.tier,(SELECT count(*) FROM league_memberships g WHERE g.group_id=m.group_id)
 FROM league_memberships m WHERE m.learner_id=$1::uuid AND m.week_start=$2::date`, owner, week).Scan(&group, &tier, &members)
	}
	err = lookup()
	if errors.Is(err, pgx.ErrNoRows) {
		// XP earned before leagues existed (or after a restore) joins idempotently; no XP means no group.
		var hasXP bool
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xp_events WHERE learner_id=$1::uuid AND local_date BETWEEN $2::date AND $3::date)`, owner, week, end).Scan(&hasXP); err != nil || !hasXP {
			return view, err
		}
		if err = s.joinExisting(ctx, owner, local); err != nil {
			return view, err
		}
		err = lookup()
	}
	if err != nil {
		return view, err
	}
	rules, err := loadLeagueRules(ctx, s.pool)
	if err != nil {
		return view, err
	}
	entries, err := s.leagueEntries(ctx, group, members)
	if err != nil {
		return view, err
	}
	ranked := RankLeague(entries)
	page, next, prev := PageLeague(ranked, offset, limit)
	ids := make([]string, 0, len(page))
	for _, e := range page {
		ids = append(ids, e.Learner)
	}
	names := map[string]string{}
	nameRows, err := s.pool.Query(ctx, `SELECT id::text,display_name FROM learners WHERE id=ANY($1::uuid[])`, ids)
	if err != nil {
		return view, err
	}
	for nameRows.Next() {
		var id, name string
		if err = nameRows.Scan(&id, &name); err != nil {
			nameRows.Close()
			return view, err
		}
		names[id] = name
	}
	nameRows.Close()
	if err = nameRows.Err(); err != nil {
		return view, err
	}
	total := len(ranked)
	tierName := rules.Tiers[min(tier, len(rules.Tiers))-1]
	view.State = "active"
	view.League = &LeagueInfo{Name: tierName, Tier: tier, Members: total, PromotionTop: rules.PromotionTop(tier, total), DemotionFrom: rules.DemotionFrom(tier, total)}
	for _, e := range ranked {
		if e.Learner == owner {
			view.Self = &LeagueSelf{Rank: e.Rank, WeeklyXP: e.XP, Zone: rules.Zone(tier, total, e.Rank)}
		}
	}
	for _, e := range page {
		name := strings.TrimSpace(names[e.Learner])
		if name == "" {
			name = "Learner"
		}
		view.Rows = append(view.Rows, LeagueRow{Rank: e.Rank, DisplayName: name, WeeklyXP: e.XP, Tie: e.Tie, Self: e.Learner == owner, Zone: rules.Zone(tier, total, e.Rank)})
	}
	view.Page = LeaguePage{Offset: offset, Limit: limit, Total: total, NextOffset: next, PrevOffset: prev}
	return view, nil
}

func (s *Service) joinExisting(ctx context.Context, owner, local string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM learners WHERE id=$1::uuid FOR UPDATE`, owner).Scan(&locked); err != nil {
		return err
	}
	slot, err := s.joinLeague(ctx, tx, owner, local)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.refreshLeague(ctx, owner, []leagueSlot{slot})
	return nil
}

func (s *Service) serveLeague(w http.ResponseWriter, r *http.Request, owner string) {
	if r.Method != http.MethodGet {
		write(w, 405, map[string]string{"error": "Method unavailable."})
		return
	}
	offset, limit := 0, 10
	for key, target := range map[string]*int{"offset": &offset, "limit": &limit} {
		if raw := r.URL.Query().Get(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 || (key == "limit" && (n < 1 || n > 30)) {
				write(w, 400, map[string]string{"error": "Choose a valid league page."})
				return
			}
			*target = n
		}
	}
	view, err := s.League(r.Context(), owner, offset, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "league read failed", "error", err)
		write(w, 503, map[string]string{"error": "League unavailable. Retry."})
		return
	}
	write(w, 200, view)
}
