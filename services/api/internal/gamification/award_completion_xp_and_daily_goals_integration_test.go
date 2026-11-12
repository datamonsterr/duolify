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

type discardObjects struct{}

func (discardObjects) Put(context.Context, string, []byte) error { return nil }

type eventSink struct {
	mu    sync.Mutex
	names []string
}

func (e *eventSink) Capture(_ string, name string, _ map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.names = append(e.names, name)
	return nil
}
func (*eventSink) Close() error { return nil }

func TestCompletionConsumerConcurrentLocalDaysAndAuthorization(t *testing.T) {
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
	auth, err := accounts.New(db.Pool, "gamification-test-secret-at-least-32-bytes", mail)
	if err != nil {
		t.Fatal(err)
	}
	sink := &eventSink{}
	svc := New(db.Pool, analytics.WithConsent(sink, analytics.DatabaseConsent(db.Pool)))
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Gamification: svc}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	call := func(method, path, body, token string) (int, map[string]any) {
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
		if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, out
	}
	login := func(email string) (string, string) {
		if status, _ := call("POST", "/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`, ""); status != 202 {
			t.Fatal(status)
		}
		status, out := call("POST", "/v1/auth/verify", `{"email":"`+email+`","age_confirmed":true,"code":"`+mail.Code(email)+`"}`, "")
		if status != 200 {
			t.Fatal(status, out)
		}
		var id string
		if err = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return out["access_token"].(string), id
	}
	token, owner := login("xp@example.test")
	otherToken, other := login("other@example.test")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE gamification_config SET base_xp=17, perfect_bonus_xp=6, default_goal=23, goal_options=ARRAY[23,46]`)
	exec(`UPDATE learner_profiles SET time_zone='America/New_York',daily_xp=23,analytics_opt_out=false WHERE learner_id=$1`, owner)
	lesson, err := playerfixture.Seed(ctx, db.Pool, discardObjects{}, owner)
	if err != nil {
		t.Fatal(err)
	}
	var course string
	if err = db.Pool.QueryRow(ctx, `SELECT u.course_id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE l.id=$1`, lesson).Scan(&course); err != nil {
		t.Fatal(err)
	}
	passed := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC) // first 01:30 in the DST overlap
	exec(`INSERT INTO lesson_completion_outbox(learner_id,course_id,lesson_id,passed_at) VALUES($1,$2,$3,$4)`, owner, course, lesson, passed)
	exec(`INSERT INTO lesson_completion_summaries(learner_id,course_id,lesson_id,summary,first_try_perfect) VALUES($1,$2,$3,'private summary',true)`, owner, course, lesson)
	start := make(chan struct{})
	failures := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { <-start; failures <- svc.Consume(ctx, owner) })
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count, total int
	var date, zone string
	if err = db.Pool.QueryRow(ctx, `SELECT count(*),sum(amount),min(local_date::text),min(time_zone) FROM xp_events WHERE learner_id=$1`, owner).Scan(&count, &total, &date, &zone); err != nil {
		t.Fatal(err)
	}
	if count != 1 || total != 23 || date != "2026-11-01" || zone != "America/New_York" {
		t.Fatal(count, total, date, zone)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM daily_goal_achievements WHERE learner_id=$1`, owner).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_entries WHERE learner_id=$1`, owner).Scan(&count); err != nil || count != 0 {
		t.Fatal("effort granted Credits", count, err)
	}
	sink.mu.Lock()
	names := append([]string{}, sink.names...)
	sink.mu.Unlock()
	for _, name := range []string{"xp_earned", "lesson_completed", "daily_goal_met"} {
		n := 0
		for _, got := range names {
			if got == name {
				n++
			}
		}
		if n != 1 {
			t.Fatal(name, names)
		}
	}
	for _, query := range []string{`UPDATE xp_events SET amount=999 WHERE learner_id=$1`, `UPDATE daily_goal_achievements SET goal_xp=999 WHERE learner_id=$1`} {
		if _, err = db.Pool.Exec(ctx, query, owner); err == nil {
			t.Fatal("mutable history", query)
		}
	}
	if status, _ := call("GET", "/v1/gamification", "", ""); status != 401 {
		t.Fatal("anonymous", status)
	}
	if status, _ := call("GET", "/v1/gamification/lessons/"+lesson, "", otherToken); status != 404 {
		t.Fatal("cross learner lesson", status)
	}
	if status, _ := call("PUT", "/v1/gamification", `{"daily_goal":46,"learner_id":"`+owner+`"}`, otherToken); status != 400 {
		t.Fatal("identity injection", status)
	}
	if status, _ := call("PUT", "/v1/gamification", `{"daily_goal":24}`, token); status != 400 {
		t.Fatal("unconfigured goal", status)
	}
	if status, out := call("GET", "/v1/gamification", "", otherToken); status != 200 || out["total_xp"] != float64(0) {
		t.Fatal("other totals", status, out, other)
	}
	// Both DST transitions, local midnight, and concurrent distinct lessons use real rows.
	addCompletion := func(at time.Time, summary bool) string {
		t.Helper()
		id := uuid.NewString()
		exec(`INSERT INTO course_lesson_stubs(id,learner_id,unit_id,authored_id,position,title,goal,status)
		SELECT $1::uuid,$2::uuid,unit_id,$1::text,(SELECT max(position)+1 FROM course_lesson_stubs WHERE unit_id=l.unit_id),'Boundary lesson','Learn','done' FROM course_lesson_stubs l WHERE id=$3::uuid`, id, owner, lesson)
		exec(`INSERT INTO lesson_completion_outbox(learner_id,course_id,lesson_id,passed_at) VALUES($1,$2,$3,$4)`, owner, course, id, at)
		if summary {
			exec(`INSERT INTO lesson_completion_summaries(learner_id,course_id,lesson_id,summary) VALUES($1,$2,$3,'private boundary summary')`, owner, course, id)
		}
		return id
	}
	for _, at := range []string{"2026-03-08T04:59:59Z", "2026-03-08T05:00:00Z", "2026-03-08T06:59:59Z", "2026-03-08T07:00:00Z", "2026-11-01T06:30:00Z", "2026-11-02T04:59:59Z", "2026-11-02T05:00:00Z"} {
		instant, parseErr := time.Parse(time.RFC3339, at)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		addCompletion(instant, true)
	}
	if err = svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	for day, want := range map[string]int{"2026-03-07": 17, "2026-03-08": 51, "2026-11-01": 57, "2026-11-02": 17} {
		if err = db.Pool.QueryRow(ctx, `SELECT sum(amount) FROM xp_events WHERE learner_id=$1 AND local_date=$2::date`, owner, day).Scan(&total); err != nil || total != want {
			t.Fatal(day, total, want, err)
		}
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM daily_goal_achievements WHERE learner_id=$1`, owner).Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate/missing goal days", count, err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO xp_events(learner_id,lesson_id,course_id,completion_id,amount,perfect,config_version,earned_at,local_date,time_zone) SELECT learner_id,lesson_id,course_id,gen_random_uuid(),amount,perfect,config_version,earned_at,local_date,time_zone FROM xp_events WHERE learner_id=$1 LIMIT 1`, owner); err == nil {
		t.Fatal("database accepted duplicate lesson award")
	}
	for _, query := range []string{`DELETE FROM xp_events WHERE learner_id=$1`, `DELETE FROM daily_goal_achievements WHERE learner_id=$1`} {
		if _, err = db.Pool.Exec(ctx, query, owner); err == nil {
			t.Fatal("history erased", query)
		}
	}
	for _, table := range []string{"xp_events", "daily_goal_achievements"} {
		if _, err = db.Pool.Exec(ctx, `TRUNCATE `+table); err == nil {
			t.Fatal("history truncated", table)
		}
	}
	// Opt-out suppresses new awards AND goal crossings, preserving durable progress.
	sink.mu.Lock()
	beforeOptOut := len(sink.names)
	sink.mu.Unlock()
	exec(`UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1`, owner)
	addCompletion(time.Now(), true)
	addCompletion(time.Now(), true)
	missingSummary := addCompletion(time.Now(), false)
	if err = svc.Consume(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if status, out := call("GET", "/v1/gamification", "", token); status != 200 || out["daily_xp"] != float64(34) || out["goal_met"] != true {
		t.Fatal("opt-out suppressed XP", status, out)
	}
	if status, _ := call("GET", "/v1/gamification/lessons/"+missingSummary, "", token); status != 409 {
		t.Fatal("awarded without summary", status)
	}
	for _, goal := range []string{"46", "23", "23"} {
		if status, _ := call("PUT", "/v1/gamification", `{"daily_goal":`+goal+`}`, token); status != 200 {
			t.Fatal("goal edit", status)
		}
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM daily_goal_achievements WHERE learner_id=$1`, owner).Scan(&count); err != nil || count != 3 {
		t.Fatal("goal edit duplicated marker", count, err)
	}
	// Invalid persisted timezone surfaces an error; it cannot silently award under UTC.
	exec(`UPDATE learner_profiles SET time_zone='invalid/timezone' WHERE learner_id=$1`, owner)
	if status, _ := call("GET", "/v1/gamification", "", token); status != 503 {
		t.Fatal("swallowed timezone error", status)
	}
	exec(`UPDATE learner_profiles SET time_zone='America/New_York' WHERE learner_id=$1`, owner)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.names) != beforeOptOut {
		t.Fatal("replay emitted", sink.names)
	}
	exec(`DELETE FROM courses WHERE id=$1`, course)
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM xp_events WHERE learner_id=$1`, owner).Scan(&count); err != nil || count != 10 {
		t.Fatal("course deletion lost earned XP", count, err)
	}
	exec(`DELETE FROM learners WHERE id=$1`, owner)
	for _, table := range []string{"xp_events", "daily_goal_achievements"} {
		if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE learner_id=$1`, owner).Scan(&count); err != nil || count != 0 {
			t.Fatal("erasure", table, count, err)
		}
	}
}
