//go:build integration

package gamification

import (
	"encoding/json"
	"fmt"
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

func TestWeeklyLeaguesRankFromDurableXPAndRebuildAfterRedisLoss(t *testing.T) {
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
	clock := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) // Monday
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
	exec(`UPDATE gamification_config SET league_group_size=3,league_promote_top=1,league_demote_bottom=1`)

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
	rowsOf := func(out map[string]any) (names []string, xp []float64) {
		for _, raw := range out["rows"].([]any) {
			row := raw.(map[string]any)
			names = append(names, row["display_name"].(string))
			xp = append(xp, row["weekly_xp"].(float64))
		}
		return names, xp
	}

	mon := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	mina, kai, linh, sam := signup("Mina"), signup("Kai"), signup("Linh"), signup("Sam")

	// Not joined: reading creates no group or membership.
	if out := league(mina, ""); out["state"] != "not_joined" || len(out["rows"].([]any)) != 0 {
		t.Fatal(out)
	}
	if scalar(`SELECT count(*) FROM league_groups`) != 0 || scalar(`SELECT count(*) FROM league_memberships`) != 0 {
		t.Fatal("read created league state")
	}
	if status, _ := call("GET", "/v1/gamification/league", "", ""); status != 401 {
		t.Fatal(status)
	}

	// Last week's XP never counts; a Sunday-night completion belongs to the week before.
	complete(sam, mon.AddDate(0, 0, -1))
	if scalar(`SELECT count(*) FROM league_memberships WHERE learner_id=$1 AND week_start='2026-11-02'`, sam.id) != 1 {
		t.Fatal("previous week membership missing")
	}

	// First eligible completion joins exactly once, even with concurrent consumers.
	complete(mina, mon)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { _ = svc.Consume(ctx, mina.id) })
	}
	wg.Wait()
	if scalar(`SELECT count(*) FROM league_memberships WHERE learner_id=$1 AND week_start='2026-11-09'`, mina.id) != 1 {
		t.Fatal("membership not unique")
	}
	complete(kai, mon.Add(time.Hour))
	complete(linh, mon.Add(2*time.Hour))
	complete(linh, mon.Add(3*time.Hour)) // Linh leads with 20 XP (base 10 each).
	complete(sam, mon.Add(4*time.Hour))  // Fourth member overflows the size-3 group.
	if got := scalar(`SELECT count(*) FROM league_groups WHERE week_start='2026-11-09'`); got != 2 {
		t.Fatalf("groups = %d", got)
	}
	if got := scalar(`SELECT max(size) FROM league_groups`); got != 3 {
		t.Fatalf("max group size = %d", got)
	}
	for _, event := range []string{"league_promoted", "league_demoted", "league_joined"} {
		if sink.count(mina.id, event) != 0 {
			t.Fatal("joining must not emit", event)
		}
	}

	// Ranking: weekly XP from Postgres, ties ordered by earlier slot, own group only.
	out := league(kai, "")
	names, xp := rowsOf(out)
	if fmt.Sprint(names) != "[Linh Mina Kai]" || fmt.Sprint(xp) != "[20 10 10]" {
		t.Fatal(names, xp, out)
	}
	if self := out["self"].(map[string]any); self["rank"] != float64(3) || self["zone"] != "none" { // lowest tier never demotes
		t.Fatal(self)
	}
	rows := out["rows"].([]any)
	if rows[0].(map[string]any)["zone"] != "promotion" || rows[1].(map[string]any)["tie"] != true || rows[2].(map[string]any)["tie"] != true || rows[2].(map[string]any)["self"] != true {
		t.Fatal(rows)
	}
	if out["week"].(map[string]any)["start"] != "2026-11-09" || out["week"].(map[string]any)["end"] != "2026-11-15" || out["as_of"] == nil {
		t.Fatal(out["week"], out["as_of"])
	}
	if n, _ := rowsOf(league(sam, "")); len(n) != 1 || n[0] != "Sam" {
		t.Fatalf("other group leaked into %v", n)
	}
	// Pagination keeps global ranks.
	page := league(kai, "?offset=1&limit=1")
	if n, _ := rowsOf(page); fmt.Sprint(n) != "[Mina]" || page["rows"].([]any)[0].(map[string]any)["rank"] != float64(2) || page["page"].(map[string]any)["next_offset"] != float64(2) || page["page"].(map[string]any)["prev_offset"] != float64(0) {
		t.Fatal(page)
	}
	if status, _ := call("GET", "/v1/gamification/league?offset=-1", "", kai.token); status != 400 {
		t.Fatal(status)
	}

	// Redis holds the ranking; flushing it changes nothing and rebuilds on demand.
	var group string
	if err = db.Pool.QueryRow(ctx, `SELECT group_id::text FROM league_memberships WHERE learner_id=$1 AND week_start='2026-11-09'`, kai.id).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if rdb.Exists(ctx, "league:"+group).Val() != 1 {
		t.Fatal("ranking was not cached")
	}
	before, _ := json.Marshal(league(kai, ""))
	if err = rdb.FlushAll(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(league(kai, ""))
	if string(before) != string(after) {
		t.Fatalf("rebuild differs:\n%s\n%s", before, after)
	}
	if rdb.Exists(ctx, "league:"+group).Val() != 1 {
		t.Fatal("ranking was not rebuilt into Redis")
	}
	// New XP after flush and with a warm cache both show up exactly once.
	complete(kai, mon.Add(5*time.Hour))
	complete(kai, mon.Add(6*time.Hour))
	if n, x := rowsOf(league(mina, "")); fmt.Sprint(n) != "[Kai Linh Mina]" || fmt.Sprint(x) != "[30 20 10]" {
		t.Fatal(n, x)
	}
	if err = rdb.FlushAll(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if n, x := rowsOf(league(mina, "")); fmt.Sprint(n) != "[Kai Linh Mina]" || fmt.Sprint(x) != "[30 20 10]" {
		t.Fatal(n, x)
	}
	// Unreachable Redis never blocks the view.
	svc.UseCache(nil)
	if n, _ := rowsOf(league(mina, "")); fmt.Sprint(n) != "[Kai Linh Mina]" {
		t.Fatal(n)
	}
	svc.UseCache(rdb)

	// Pre-existing XP without membership joins idempotently on read; a new week is a new membership.
	exec(`DELETE FROM league_memberships WHERE learner_id=$1 AND week_start='2026-11-09'`, kai.id)
	if out := league(kai, ""); out["state"] != "active" || scalar(`SELECT count(*) FROM league_memberships WHERE learner_id=$1 AND week_start='2026-11-09'`, kai.id) != 1 {
		t.Fatal(out)
	}
	clock = time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	if out := league(kai, ""); out["state"] != "not_joined" || out["week"].(map[string]any)["start"] != "2026-11-16" {
		t.Fatal(out)
	}
	complete(kai, clock)
	var tier int
	if err = db.Pool.QueryRow(ctx, `SELECT tier FROM league_memberships WHERE learner_id=$1 AND week_start='2026-11-16'`, kai.id).Scan(&tier); err != nil || tier != 1 {
		t.Fatal(tier, err)
	}
	if got := scalar(`SELECT count(*) FROM league_memberships WHERE learner_id=$1`, kai.id); got != 2 {
		t.Fatalf("memberships = %d", got)
	}

	// Erasure removes the Learner's membership and ranking row; groups hold no personal data.
	exec(`DELETE FROM learners WHERE id=$1`, linh.id)
	if scalar(`SELECT count(*) FROM league_memberships WHERE learner_id=$1`, linh.id) != 0 {
		t.Fatal("membership survived account deletion")
	}
}
