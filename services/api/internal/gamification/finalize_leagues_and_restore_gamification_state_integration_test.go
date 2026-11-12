//go:build integration

package gamification

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestWeeklyLeaguesFinalizeOnceAndRestoreRebuildsProjections(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	redisContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{Image: "redis:7-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForLog("Ready to accept connections")},
		Started:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, redisContainer)
	endpoint, err := redisContainer.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	rdb, err := cache.New("redis://" + endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rdb.Close() }()
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mail := &accounts.MailSink{}
	auth, err := accounts.New(db.Pool, "league-test-secret-at-least-32-bytes!!", mail)
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	svc := New(db.Pool, analytics.WithConsent(sink, analytics.DatabaseConsent(db.Pool)))
	svc.UseCache(rdb)
	clock := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC) // Monday, before UTC-12 reaches it
	svc.now = func() time.Time { return clock }
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Gamification: svc}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	call := func(method, path, body, token string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE gamification_config SET league_group_size=2,league_promote_top=1,league_demote_bottom=1,freeze_milestone_days=3`)

	type learner struct {
		id, token, lesson, course string
	}
	signup := func(name string) learner {
		t.Helper()
		email := strings.ToLower(name) + "@example.test"
		if status, _ := call("POST", "/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`, ""); status != 202 {
			t.Fatal(status)
		}
		status, out := call("POST", "/v1/auth/verify", `{"email":"`+email+`","age_confirmed":true,"code":"`+mail.Code(email)+`"}`, "")
		if status != 200 {
			t.Fatal(status, out)
		}
		l := learner{token: out["access_token"].(string)}
		if err := db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&l.id); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE learner_profiles SET time_zone='UTC',analytics_opt_out=false WHERE learner_id=$1`, l.id)
		exec(`UPDATE learners SET display_name=$2 WHERE id=$1`, l.id, name)
		if l.lesson, err = playerfixture.Seed(ctx, db.Pool, discardObjects{}, l.id); err != nil {
			t.Fatal(err)
		}
		if err = db.Pool.QueryRow(ctx, `SELECT u.course_id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE l.id=$1`, l.lesson).Scan(&l.course); err != nil {
			t.Fatal(err)
		}
		return l
	}
	complete := func(l learner, at time.Time) {
		t.Helper()
		id := uuid.NewString()
		exec(`INSERT INTO course_lesson_stubs(id,learner_id,unit_id,authored_id,position,title,goal,status)
		SELECT $1::uuid,$2::uuid,unit_id,$1::text,(SELECT max(position)+1 FROM course_lesson_stubs WHERE unit_id=l.unit_id),'League lesson','Learn','done' FROM course_lesson_stubs l WHERE id=$3::uuid`, id, l.id, l.lesson)
		exec(`INSERT INTO lesson_completion_outbox(learner_id,course_id,lesson_id,passed_at) VALUES($1,$2,$3,$4)`, l.id, l.course, id, at)
		exec(`INSERT INTO lesson_completion_summaries(learner_id,course_id,lesson_id,summary) VALUES($1,$2,$3,'private league summary')`, l.id, l.course, id)
		if err := svc.Consume(ctx, l.id); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(query string, args ...any) (n int) {
		t.Helper()
		if err := db.Pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	league := func(l learner, query string) map[string]any {
		t.Helper()
		status, out := call("GET", "/v1/gamification/league"+query, "", l.token)
		if status != 200 {
			t.Fatal(status, out)
		}
		return out
	}
	day := func(d int, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, time.UTC) }
	mina, kai, linh, sam, ana := signup("Mina"), signup("Kai"), signup("Linh"), signup("Sam"), signup("Ana")
	lastWeek := func(l learner) map[string]any {
		t.Helper()
		out := league(l, "")
		raw, _ := json.Marshal(out)
		if strings.Contains(strings.ToLower(string(raw)), "credit") {
			t.Fatal("league response mentions credits", string(raw))
		}
		last, _ := out["last_week"].(map[string]any)
		return last
	}

	// Week 1 (Nov 2-8): groups of two per tier. Mina 30 XP over three days, Kai 20, Linh 20, Sam 10.
	for _, d := range []int{3, 4, 5} {
		complete(mina, day(d, 9))
	}
	complete(kai, day(3, 9))
	complete(kai, day(4, 9))
	complete(linh, day(3, 10))
	complete(linh, day(4, 10))
	complete(sam, day(3, 11))
	if scalar(`SELECT count(*) FROM league_groups WHERE week_start='2026-11-02'`) != 2 {
		t.Fatal("week one groups")
	}

	// Before every time zone has left Sunday nothing is final.
	if err = svc.FinalizeLeagues(ctx); err != nil {
		t.Fatal(err)
	}
	if scalar(`SELECT count(*) FROM league_week_results`) != 0 || scalar(`SELECT count(*) FROM league_groups WHERE finalized_at IS NOT NULL`) != 0 {
		t.Fatal("finalized before the close")
	}
	if last := lastWeek(mina); last["state"] != "closing" || last["outcome"] != nil {
		t.Fatal(last)
	}
	if last := lastWeek(ana); last["state"] != "none" {
		t.Fatal(last)
	}
	// Mina earns XP in week 2 before week 1 is final: provisional tier, corrected at close.
	complete(mina, day(9, 9))
	if scalar(`SELECT tier FROM league_memberships WHERE learner_id=$1 AND week_start='2026-11-09'`, mina.id) != 1 {
		t.Fatal("expected provisional tier 1")
	}

	// Rollover one: retries, concurrent workers and repeats commit results exactly once.
	clock = day(9, 12)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { _ = svc.FinalizeLeagues(ctx) })
	}
	wg.Wait()
	for range 2 {
		if err = svc.FinalizeLeagues(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if scalar(`SELECT count(*) FROM league_week_results WHERE week_start='2026-11-02'`) != 4 || scalar(`SELECT count(*) FROM league_groups WHERE finalized_at IS NOT NULL`) != 2 {
		t.Fatal("results not committed exactly once")
	}
	for _, l := range []learner{mina, linh} {
		if sink.count(l.id, "league_promoted") != 1 {
			t.Fatal("promotion not emitted once", l.id)
		}
	}
	for _, l := range []learner{kai, sam} {
		if sink.count(l.id, "league_promoted")+sink.count(l.id, "league_demoted") != 0 {
			t.Fatal("stayed learner emitted", l.id)
		}
	}
	last := lastWeek(mina)
	if last["state"] != "final" || last["outcome"] != "promoted" || last["rank"] != float64(1) || last["members"] != float64(2) || last["weekly_xp"] != float64(30) || last["league"] != "Dawn" || last["next_league"] != "Sun" {
		t.Fatal(last)
	}
	if last = lastWeek(kai); last["outcome"] != "stayed" || last["next_league"] != "Dawn" {
		t.Fatal(last)
	}
	if scalar(`SELECT tier FROM league_memberships WHERE learner_id=$1 AND week_start='2026-11-09'`, mina.id) != 2 {
		t.Fatal("early week-two membership kept the stale tier")
	}
	complete(linh, day(9, 13))
	complete(linh, day(9, 14))
	if scalar(`SELECT tier FROM league_memberships WHERE learner_id=$1 AND week_start='2026-11-09'`, linh.id) != 2 || scalar(`SELECT count(DISTINCT group_id) FROM league_memberships WHERE tier=2 AND week_start='2026-11-09'`) != 1 {
		t.Fatal("promoted learners did not meet in the Sun league")
	}

	// Rollover two: Linh (20 XP) moves up again, Mina (10 XP) moves down.
	clock = day(16, 12)
	if err = svc.Rollover(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = svc.FinalizeLeagues(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if sink.count(linh.id, "league_promoted") != 2 || sink.count(mina.id, "league_demoted") != 1 || sink.count(mina.id, "league_promoted") != 1 {
		t.Fatal("second rollover events")
	}
	// Nov 16 starts week three, so the prior week is week two.
	if last = lastWeek(mina); last["week"].(map[string]any)["start"] != "2026-11-09" || last["outcome"] != "demoted" || last["next_league"] != "Dawn" || last["league"] != "Sun" {
		t.Fatal(last)
	}
	event := sink.last("league_demoted")
	for key, value := range event {
		if s, ok := value.(string); ok && strings.Contains(s, "private") {
			t.Fatal("private text in event", key)
		}
	}
	if event["rank"] == nil || event["from_tier"] == nil || event["to_tier"] == nil || event["event_id"] == nil || event["source"] != "api" || event["platform"] != "web" {
		t.Fatal(event)
	}

	// Restore: a dump holds xp_events; every projection is rebuilt from them with no new reward.
	snapshot := func() string {
		t.Helper()
		var out string
		if err := db.Pool.QueryRow(ctx, `SELECT jsonb_build_object(
		 'xp',(SELECT jsonb_agg(to_jsonb(x)-'id'-'created_at') FROM (SELECT * FROM xp_events ORDER BY learner_id,earned_at,id) x),
		 'streak',(SELECT jsonb_agg(jsonb_build_array(learner_id,current_days,longest_days,last_active_date,decided_through) ORDER BY learner_id) FROM streak_state),
		 'inventory',(SELECT jsonb_agg(jsonb_build_array(learner_id,held) ORDER BY learner_id) FROM streak_freeze_inventory),
		 'protections',(SELECT jsonb_agg(jsonb_build_array(learner_id,protected_on,remaining,streak_length) ORDER BY learner_id,protected_on) FROM streak_day_protections),
		 'milestones',(SELECT jsonb_agg(jsonb_build_array(learner_id,milestone_days,reached_on,granted) ORDER BY learner_id,reached_on) FROM streak_freeze_milestones),
		 'results',(SELECT jsonb_agg(jsonb_build_array(learner_id,week_start,tier_from,tier_to,final_rank,members,weekly_xp,outcome,id) ORDER BY learner_id,week_start) FROM league_week_results),
		 'memberships',(SELECT jsonb_agg(jsonb_build_array(learner_id,week_start,tier) ORDER BY learner_id,week_start) FROM league_memberships))::text`).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	want := snapshot()
	if !strings.Contains(want, `"promoted"`) || scalar(`SELECT count(*) FROM streak_day_protections`) == 0 {
		t.Fatal("fixture lacks league results or consumed freezes", want)
	}
	streakEvents := [3]int{sink.count("", "streak_extended"), sink.count("", "streak_broken"), sink.count("", "streak_freeze_used")}
	promoted, demoted := sink.count("", "league_promoted"), sink.count("", "league_demoted")
	exec(`TRUNCATE streak_state,streak_days,streak_breaks,streak_freeze_inventory,streak_freeze_milestones,streak_day_protections,league_week_results`)
	exec(`UPDATE league_groups SET finalized_at=NULL`)
	for range 2 {
		if err = svc.Rollover(ctx); err != nil {
			t.Fatal(err)
		}
		if err = svc.FinalizeLeagues(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := snapshot(); got != want {
		t.Fatalf("restored projections differ:\n%s\n%s", got, want)
	}
	// Rebuilt streak history is silent; results re-sent after a restore reuse their deterministic event ids (the dedupe key).
	if sink.count("", "streak_extended") != streakEvents[0] || sink.count("", "streak_broken") != streakEvents[1] || sink.count("", "streak_freeze_used") != streakEvents[2] {
		t.Fatal("restore replayed streak analytics")
	}
	if sink.count("", "league_promoted") != 2*promoted || sink.count("", "league_demoted") != 2*demoted {
		t.Fatal("restore did not resend each result once")
	}
	ids := map[string]int{}
	for _, name := range []string{"league_promoted", "league_demoted"} {
		sink.mu.Lock()
		for _, e := range sink.events {
			if e.name == name {
				ids[e.props["event_id"].(string)]++
			}
		}
		sink.mu.Unlock()
	}
	for id, n := range ids {
		if n != 2 {
			t.Fatal("event id not stable across restore", id, n)
		}
	}
	if last = lastWeek(mina); last == nil || last["state"] != "final" {
		t.Fatal(last)
	}
	// No path in this ticket touches Credits.
	if scalar(`SELECT count(*) FROM credit_entries`) != 0 {
		t.Fatal("credits were granted")
	}
}
