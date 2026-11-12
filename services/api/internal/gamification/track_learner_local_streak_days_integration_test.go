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

type capturedEvent struct {
	distinct, name string
	props          map[string]any
}
type recordingSink struct {
	mu     sync.Mutex
	events []capturedEvent
}

func (r *recordingSink) Capture(distinct, name string, props map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, capturedEvent{distinct, name, props})
	return nil
}
func (*recordingSink) Close() error { return nil }
func (r *recordingSink) count(distinct, name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.name == name && (distinct == "" || e.distinct == distinct) {
			n++
		}
	}
	return n
}
func (r *recordingSink) last(name string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].name == name {
			return r.events[i].props
		}
	}
	return nil
}

func TestStreakDaysAcrossDSTTimezonesRolloverAndLateVerification(t *testing.T) {
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
	auth, err := accounts.New(db.Pool, "streak-test-secret-at-least-32-bytes!!", mail)
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
	token, owner := login("streak@example.test")
	otherToken, other := login("streak-other@example.test")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE learner_profiles SET time_zone='America/New_York',analytics_opt_out=false WHERE learner_id=$1`, owner)
	exec(`UPDATE learner_profiles SET time_zone='America/New_York',analytics_opt_out=true WHERE learner_id=$1`, other)
	lesson, err := playerfixture.Seed(ctx, db.Pool, discardObjects{}, owner)
	if err != nil {
		t.Fatal(err)
	}
	var course string
	if err = db.Pool.QueryRow(ctx, `SELECT u.course_id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE l.id=$1`, lesson).Scan(&course); err != nil {
		t.Fatal(err)
	}
	complete := func(at time.Time) {
		t.Helper()
		id := uuid.NewString()
		exec(`INSERT INTO course_lesson_stubs(id,learner_id,unit_id,authored_id,position,title,goal,status)
		SELECT $1::uuid,$2::uuid,unit_id,$1::text,(SELECT max(position)+1 FROM course_lesson_stubs WHERE unit_id=l.unit_id),'Streak lesson','Learn','done' FROM course_lesson_stubs l WHERE id=$3::uuid`, id, owner, lesson)
		exec(`INSERT INTO lesson_completion_outbox(learner_id,course_id,lesson_id,passed_at) VALUES($1,$2,$3,$4)`, owner, course, id, at)
		exec(`INSERT INTO lesson_completion_summaries(learner_id,course_id,lesson_id,summary) VALUES($1,$2,$3,'private streak summary')`, owner, course, id)
	}
	view := func() StreakView {
		t.Helper()
		v, err := svc.Streak(ctx, owner, "")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	rollover := func() {
		t.Helper()
		if err := svc.Rollover(ctx); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(label string, v StreakView, current, longest int, pending bool) {
		t.Helper()
		if v.CurrentDays != current || v.LongestDays != longest || v.RolloverPending != pending {
			t.Fatalf("%s: got current=%d longest=%d pending=%v", label, v.CurrentDays, v.LongestDays, v.RolloverPending)
		}
	}

	// Empty state is a valid answer, not an error.
	expect("empty", view(), 0, 0, false)

	// 25-hour DST-end day: two lessons in the same local day, 01:30 EDT and 15:00 EST, extend once.
	complete(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC))
	complete(time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC))
	clock = time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC)
	if err = svc.Consume(ctx, owner); err != nil {
		t.Fatal(err)
	}
	expect("dst day", view(), 1, 1, false)
	if sink.count(owner, "streak_extended") != 1 {
		t.Fatal("same local day extended twice", sink.events)
	}

	// Concurrent completions on the next local day (starts 05:00Z) extend exactly once.
	complete(time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC))
	complete(time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC))
	clock = time.Date(2026, 11, 2, 21, 0, 0, 0, time.UTC)
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
	expect("concurrent", view(), 2, 2, false)
	if sink.count(owner, "streak_extended") != 2 {
		t.Fatal("concurrent events extended more than once per day", sink.events)
	}
	if props := sink.last("streak_extended"); props["streak_length"] != 2 || props["source"] != "api" {
		t.Fatal("extended props", props)
	}

	// Next morning the worker has not run yet: show stored state with a pending marker, never guess.
	clock = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)
	v := view()
	expect("pending", v, 2, 2, true)
	if v.TodayDone || v.LocalDate != "2026-11-03" || v.TimeZone != "America/New_York" || v.AsOf.IsZero() {
		t.Fatal("pending view", v)
	}
	rollover()
	rollover() // idempotent
	expect("rolled, nothing missed", view(), 2, 2, false)
	if sink.count(owner, "streak_broken") != 0 {
		t.Fatal("broke an active streak")
	}

	// Two missed days: reset, longest kept, one broken event however often the worker runs.
	clock = time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC)
	rollover()
	rollover()
	expect("missed", view(), 0, 2, false)
	if sink.count(owner, "streak_broken") != 1 {
		t.Fatal("broken events", sink.events)
	}
	if props := sink.last("streak_broken"); props["previous_length"] != 2 {
		t.Fatal("broken props", props)
	}

	// Delayed offline verification of a Nov 3 completion reconciles to the canonical dated event.
	complete(time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC))
	if err = svc.Consume(ctx, owner); err != nil {
		t.Fatal(err)
	}
	late := view()
	expect("late verification", late, 0, 3, false)
	states := map[string]string{}
	for _, day := range late.Calendar {
		states[day.Date] = day.State
	}
	if states["2026-11-03"] != "done" || states["2026-11-04"] != "missed" || states["2026-11-05"] != "today" {
		t.Fatal("calendar", states)
	}

	// Time zone change: canonical dates keep their zone, the decided horizon never moves back.
	clock = time.Date(2026, 11, 6, 2, 0, 0, 0, time.UTC) // Nov 6 11:00 in Tokyo, Nov 5 21:00 in New York
	exec(`UPDATE learner_profiles SET time_zone='Asia/Tokyo' WHERE learner_id=$1`, owner)
	complete(clock)
	if err = svc.Consume(ctx, owner); err != nil {
		t.Fatal(err)
	}
	expect("tokyo", view(), 1, 3, false)
	brokenBefore := sink.count(owner, "streak_broken")
	exec(`UPDATE learner_profiles SET time_zone='America/New_York' WHERE learner_id=$1`, owner)
	rollover()
	rollover()
	back := view()
	expect("zone changed back", back, 1, 3, false)
	if back.LocalDate != "2026-11-05" || sink.count(owner, "streak_broken") != brokenBefore {
		t.Fatal("zone change double counted", back, sink.events)
	}
	var days, maxDecided int
	if err = db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM streak_days WHERE learner_id=$1),(SELECT (decided_through='2026-11-05')::int FROM streak_state WHERE learner_id=$1)`, owner).Scan(&days, &maxDecided); err != nil || days != 4 || maxDecided != 1 {
		t.Fatal(days, maxDecided, err)
	}

	// Projection rebuilds silently from xp_events when the streak tables are lost.
	extendedBefore := sink.count(owner, "streak_extended")
	exec(`DELETE FROM streak_breaks WHERE learner_id=$1`, owner)
	exec(`DELETE FROM streak_days WHERE learner_id=$1`, owner)
	exec(`DELETE FROM streak_state WHERE learner_id=$1`, owner)
	rollover()
	expect("rebuilt", view(), 1, 3, false)
	if sink.count(owner, "streak_extended") != extendedBefore {
		t.Fatal("rebuild re-emitted analytics")
	}

	// Authorization and API shape.
	if status, _ := call("GET", "/v1/gamification/streak", "", ""); status != 401 {
		t.Fatal("anonymous", status)
	}
	status, out := call("GET", "/v1/gamification/streak?learner_id="+owner, "", otherToken)
	if status != 200 || out["current_days"] != float64(0) || out["longest_days"] != float64(0) {
		t.Fatal("cross learner", status, out, other)
	}
	status, out = call("GET", "/v1/gamification/streak", "", token)
	if status != 200 || out["current_days"] != float64(1) || out["longest_days"] != float64(3) || out["month"] != "2026-11" {
		t.Fatal("owner", status, out)
	}
	if status, _ = call("GET", "/v1/gamification/streak?month=2026-13", "", token); status != 400 {
		t.Fatal("bad month", status)
	}
	if status, _ = call("POST", "/v1/gamification/streak", `{}`, token); status != 405 {
		t.Fatal("method", status)
	}
	// Opted-out learner emits nothing even with completions.
	if sink.count(other, "streak_extended") != 0 {
		t.Fatal("opt-out ignored")
	}
	for _, e := range sink.events {
		raw, _ := json.Marshal(e.props)
		if strings.Contains(string(raw), "private") {
			t.Fatal("learner text in analytics", e)
		}
	}
	var credits int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_entries WHERE learner_id=$1`, owner).Scan(&credits); err != nil || credits != 0 {
		t.Fatal("streak granted Credits", credits, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = svc.Rollover(cancelled); err == nil {
		t.Fatal("rollover swallowed a canceled context")
	}
}
