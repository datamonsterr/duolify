//go:build integration

package gamification

import (
	"context"
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
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestStreakFreezesEarnedOnceCappedAndAutoAppliedAtRollover(t *testing.T) {
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
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mail := &accounts.MailSink{}
	auth, err := accounts.New(db.Pool, "freeze-test-secret-at-least-32-bytes!!", mail)
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	svc := New(db.Pool, analytics.WithConsent(sink, analytics.DatabaseConsent(db.Pool)))
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
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
	email := "freeze@example.test"
	if status, _ := call("POST", "/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`, ""); status != 202 {
		t.Fatal(status)
	}
	status, out := call("POST", "/v1/auth/verify", `{"email":"`+email+`","age_confirmed":true,"code":"`+mail.Code(email)+`"}`, "")
	if status != 200 {
		t.Fatal(status, out)
	}
	token := out["access_token"].(string)
	var owner string
	if err = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE learner_profiles SET time_zone='UTC',analytics_opt_out=false WHERE learner_id=$1`, owner)
	exec(`UPDATE gamification_config SET freeze_milestone_days=7,freeze_cap=1`)
	lesson, err := playerfixture.Seed(ctx, db.Pool, discardObjects{}, owner)
	if err != nil {
		t.Fatal(err)
	}
	var course string
	if err = db.Pool.QueryRow(ctx, `SELECT u.course_id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE l.id=$1`, lesson).Scan(&course); err != nil {
		t.Fatal(err)
	}
	complete := func(day int) {
		t.Helper()
		at := time.Date(2026, 11, day, 12, 0, 0, 0, time.UTC)
		id := uuid.NewString()
		exec(`INSERT INTO course_lesson_stubs(id,learner_id,unit_id,authored_id,position,title,goal,status)
		SELECT $1::uuid,$2::uuid,unit_id,$1::text,(SELECT max(position)+1 FROM course_lesson_stubs WHERE unit_id=l.unit_id),'Freeze lesson','Learn','done' FROM course_lesson_stubs l WHERE id=$3::uuid`, id, owner, lesson)
		exec(`INSERT INTO lesson_completion_outbox(learner_id,course_id,lesson_id,passed_at) VALUES($1,$2,$3,$4)`, owner, course, id, at)
		exec(`INSERT INTO lesson_completion_summaries(learner_id,course_id,lesson_id,summary) VALUES($1,$2,$3,'private freeze summary')`, owner, course, id)
		clock = at.Add(time.Hour)
		if err := svc.Consume(ctx, owner); err != nil {
			t.Fatal(err)
		}
	}
	rollover := func() {
		t.Helper()
		if err := svc.Rollover(ctx); err != nil {
			t.Fatal(err)
		}
	}
	concurrently := func(n int, fn func() error) {
		t.Helper()
		start := make(chan struct{})
		failures := make(chan error, n)
		var wg sync.WaitGroup
		for range n {
			wg.Go(func() { <-start; failures <- fn() })
		}
		close(start)
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	held := func() (n int) {
		t.Helper()
		if err := db.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT held FROM streak_freeze_inventory WHERE learner_id=$1),0)`, owner).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	count := func(query string) (n int) {
		t.Helper()
		if err := db.Pool.QueryRow(ctx, query, owner).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	view := func() StreakView {
		t.Helper()
		v, err := svc.Streak(ctx, owner, "")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	// Days 1-6 earn nothing; day 7 grants exactly one freeze, however many consumers race.
	for day := 1; day <= 6; day++ {
		complete(day)
	}
	if held() != 0 || sink.count(owner, "streak_freeze_earned") != 0 {
		t.Fatal("freeze before milestone")
	}
	complete(7)
	concurrently(16, func() error { return svc.Consume(ctx, owner) })
	if held() != 1 || count(`SELECT count(*) FROM streak_freeze_milestones WHERE learner_id=$1 AND granted`) != 1 || sink.count(owner, "streak_freeze_earned") != 1 {
		t.Fatal("milestone grant", held(), sink.events)
	}
	if props := sink.last("streak_freeze_earned"); props["reason"] != "streak" || props["streak_length"] != 7 || props["source"] != "api" || props["event_id"] == nil {
		t.Fatal("earned props", props)
	}
	v := view()
	if v.Freezes.Held != 1 || v.Freezes.Cap != 1 || v.Freezes.MilestoneDays != 7 || v.CurrentDays != 7 {
		t.Fatal("view inventory", v.Freezes, v.CurrentDays)
	}

	// Duplicate milestone or day-application rows are rejected by the database.
	if _, err = db.Pool.Exec(ctx, `INSERT INTO streak_freeze_milestones(learner_id,kind,milestone_days,run_started_on,reached_on,granted) VALUES($1,'streak',7,'2026-11-01','2026-11-07',true)`, owner); err == nil {
		t.Fatal("duplicate milestone accepted")
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE streak_freeze_inventory SET held=-1 WHERE learner_id=$1`, owner); err == nil {
		t.Fatal("negative inventory accepted")
	}

	// Day 8 is missed. Concurrent rollovers consume the freeze once; the streak is preserved, not incremented.
	clock = time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	concurrently(16, func() error { return svc.Rollover(ctx) })
	rollover()
	if held() != 0 || count(`SELECT count(*) FROM streak_day_protections WHERE learner_id=$1 AND protected_on='2026-11-08'`) != 1 || sink.count(owner, "streak_freeze_used") != 1 {
		t.Fatal("protection", held(), sink.events)
	}
	if sink.count(owner, "streak_broken") != 0 {
		t.Fatal("protected day broke the streak")
	}
	if props := sink.last("streak_freeze_used"); props["remaining"] != 0 || props["protected_on"] != "2026-11-08" || props["source"] != "api" {
		t.Fatal("used props", props)
	}
	v = view()
	if v.CurrentDays != 7 || v.Freezes.Held != 0 || len(v.Notices) != 1 || v.Notices[0].ProtectedOn != "2026-11-08" || v.Notices[0].Remaining != 0 || v.Notices[0].StreakLength != 7 {
		t.Fatal("protected view", v.CurrentDays, v.Freezes, v.Notices)
	}
	states := map[string]string{}
	for _, d := range v.Calendar {
		states[d.Date] = d.State
	}
	if states["2026-11-08"] != "frozen" || states["2026-11-07"] != "done" {
		t.Fatal("calendar", states)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO streak_day_protections(learner_id,source,protected_on,remaining,streak_length) VALUES($1,'friend_streak','2026-11-08',0,7)`, owner); err == nil {
		t.Fatal("second protection for the same learner/day accepted")
	}

	// Day 9 extends to 8 (frozen day adds nothing). Day 10 is missed without a freeze: the streak breaks.
	complete(9)
	if v = view(); v.CurrentDays != 8 {
		t.Fatal("after frozen day", v.CurrentDays)
	}
	clock = time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	rollover()
	rollover()
	if v = view(); v.CurrentDays != 0 || v.LongestDays != 8 || sink.count(owner, "streak_broken") != 1 || sink.count(owner, "streak_freeze_used") != 1 {
		t.Fatal("break without freeze", v.CurrentDays, v.LongestDays, sink.events)
	}

	// A fresh run of 14 days: the 7th grants the freeze (below cap 1), the 14th overflows with no grant and no Credits.
	for day := 11; day <= 24; day++ {
		complete(day)
	}
	if held() != 1 || sink.count(owner, "streak_freeze_earned") != 2 {
		t.Fatal("second milestone", held(), sink.count(owner, "streak_freeze_earned"))
	}
	if count(`SELECT count(*) FROM streak_freeze_milestones WHERE learner_id=$1 AND NOT granted AND milestone_days=14`) != 1 {
		t.Fatal("overflow milestone not recorded once")
	}
	if count(`SELECT count(*) FROM credit_entries WHERE learner_id=$1`) != 0 {
		t.Fatal("freeze flow granted Credits")
	}

	// No purchase or manual activation path exists.
	for _, path := range []string{"/v1/gamification/streak/freeze", "/v1/gamification/freezes", "/v1/gamification/streak/freezes/activate", "/v1/gamification/streak/freezes/buy"} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			if status, _ := call(method, path, `{}`, token); status != 404 && status != 405 {
				t.Fatal("freeze path exists", method, path, status)
			}
		}
	}
	if status, _ := call("POST", "/v1/gamification/streak", `{"activate_freeze":true}`, token); status != 405 {
		t.Fatal("streak accepted a write", status)
	}
	status, out = call("GET", "/v1/gamification/streak", "", token)
	freezes, _ := out["freezes"].(map[string]any)
	if status != 200 || freezes["held"] != float64(1) || freezes["cap"] != float64(1) || freezes["milestone_days"] != float64(7) {
		t.Fatal("api freezes", status, out)
	}

	// Backup restore recomputation: lose every streak/freeze row; the rollover worker restores identical state silently.
	protections := count(`SELECT count(*) FROM streak_day_protections WHERE learner_id=$1`)
	milestones := count(`SELECT count(*) FROM streak_freeze_milestones WHERE learner_id=$1`)
	earnedBefore, usedBefore := sink.count(owner, "streak_freeze_earned"), sink.count(owner, "streak_freeze_used")
	for _, table := range []string{"streak_day_protections", "streak_freeze_milestones", "streak_freeze_inventory", "streak_breaks", "streak_days", "streak_state"} {
		exec(`DELETE FROM `+table+` WHERE learner_id=$1`, owner)
	}
	rollover()
	if held() != 1 || count(`SELECT count(*) FROM streak_day_protections WHERE learner_id=$1`) != protections || count(`SELECT count(*) FROM streak_freeze_milestones WHERE learner_id=$1`) != milestones {
		t.Fatal("recomputed freeze state differs", held(), protections, milestones)
	}
	if sink.count(owner, "streak_freeze_earned") != earnedBefore || sink.count(owner, "streak_freeze_used") != usedBefore {
		t.Fatal("silent rebuild re-emitted analytics")
	}
	for _, e := range sink.events {
		raw, _ := json.Marshal(e.props)
		if strings.Contains(string(raw), "private") {
			t.Fatal("learner text in analytics", e)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = svc.Rollover(cancelled); err == nil {
		t.Fatal("rollover swallowed a canceled context")
	}
}
