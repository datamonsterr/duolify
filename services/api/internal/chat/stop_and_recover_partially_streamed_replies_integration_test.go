//go:build integration

package chat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/chat"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	backend "github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
)

// stopEngine scripts streams by the last learner message; "hold" streams wait for cancellation.
type stopEngine struct {
	enginev1.UnimplementedEngineServiceServer
	calls     atomic.Int32
	cancelled atomic.Int32
}

func (e *stopEngine) Chat(r *enginev1.ChatRequest, stream enginev1.EngineService_ChatServer) error {
	e.calls.Add(1)
	text := r.Messages[len(r.Messages)-1].Content
	send := func(s string) error {
		return stream.Send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_Delta{Delta: s}})
	}
	usage := func(estimated bool, provider, model string) error {
		return stream.Send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_Usage{Usage: &enginev1.Usage{Provider: provider, Model: model, InputTokens: 120, OutputTokens: 6, Estimated: estimated}}})
	}
	typed := func(code string, retryable bool) error {
		return stream.Send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_StreamError{StreamError: &enginev1.ChatStreamError{Code: code, Retryable: retryable}}})
	}
	hold := func() error {
		<-stream.Context().Done()
		e.cancelled.Add(1)
		return stream.Context().Err()
	}
	switch {
	case strings.HasPrefix(text, "hold-silent"):
		return hold()
	case strings.HasPrefix(text, "hold"):
		_ = send("Moons ")
		_ = send("orbit ")
		return hold()
	case strings.HasPrefix(text, "idle"):
		_ = send("Moons ")
		_ = send("orbit ")
		_ = typed("idle_timeout", true)
		return usage(true, r.Limits.Provider, r.Limits.Model)
	case strings.HasPrefix(text, "first-timeout"):
		_ = typed("first_token_timeout", true)
		return nil
	case strings.HasPrefix(text, "fallback"):
		_ = send("Planets ")
		_ = send("orbit.")
		return usage(false, r.Limits.FallbackProvider, r.Limits.FallbackModel)
	case strings.HasPrefix(text, "no-usage"):
		_ = send("Planets ")
		return send("orbit.")
	case strings.HasPrefix(text, "quick"):
		time.Sleep(20 * time.Millisecond)
		_ = send("Planets ")
		_ = send("orbit.")
		return usage(false, r.Limits.Provider, r.Limits.Model)
	}
	_ = send("Planets ")
	return usage(false, r.Limits.Provider, r.Limits.Model)
}

func TestStopAndRecoverPartiallyStreamedRepliesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("tutor"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key", 5), mail)
	if err != nil {
		t.Fatal(err)
	}
	lis, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture := new(stopEngine)
	rpc := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(rpc, fixture)
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	engine, err := backend.New("http://"+lis.Addr().String(), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	events := new(recorder)
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, events)
	courseService := courses.New(courses.Options{Pool: db.Pool, Environment: "test", Financial: financial, Events: events})
	tutor := chat.New(chat.Options{Pool: db.Pool, Engine: engine, Financial: financial, Profiles: courseService, Events: events})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), Credits: financial, Tutor: tutor}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	call := func(method, path string, body any, token string) (int, string) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
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
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(out)
	}
	asMap := func(raw string) map[string]any {
		var m map[string]any
		_ = json.Unmarshal([]byte(raw), &m)
		return m
	}
	login := func(email string) (string, string) {
		if code, _ := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); code != 202 {
			t.Fatal(code)
		}
		code, raw := call("POST", "/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)}, "")
		if code != 200 {
			t.Fatal(code, raw)
		}
		token := asMap(raw)["access_token"].(string)
		claims, err := auth.ParseAccess(token)
		if err != nil {
			t.Fatal(err)
		}
		return token, claims.Subject
	}
	fund := func(owner string, amount int) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,$2,'purchase',$3,$4)`, owner, amount, "fund-"+owner+fmt.Sprint(time.Now().UnixNano()), cat.Config().Version); err != nil {
			t.Fatal(err)
		}
	}
	seedCourse := func(owner, title string) string {
		t.Helper()
		var quote, creation, course string
		if err := db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) VALUES($1::uuid,'seed-ref',$2,'{}',now(),now()+interval '10 minutes') RETURNING id::text`, owner, cat.Config().Version).Scan(&quote); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1::uuid,gen_random_uuid(),1,$2::uuid,'{}','enrolled') RETURNING id::text`, owner, quote).Scan(&creation); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `WITH c AS (INSERT INTO courses(learner_id,creation_id,title,description) VALUES($1::uuid,$2::uuid,$3,'Sky course') RETURNING id),
		 u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,c.id,'u1',0,'Unit' FROM c RETURNING id),
		 e AS (INSERT INTO course_enrollments(learner_id,course_id) SELECT $1::uuid,c.id FROM c)
		 SELECT id::text FROM c`, owner, creation, title).Scan(&course); err != nil {
			t.Fatal(err)
		}
		return course
	}
	// accepted returns a fresh accepted quote for a message.
	accepted := func(token, course, message string) string {
		t.Helper()
		code, raw := call("POST", "/v1/courses/"+course+"/tutor/quote", map[string]any{"class": "short", "message": message}, token)
		if code != 200 {
			t.Fatal(code, raw)
		}
		id := asMap(raw)["id"].(string)
		if c, r := call("POST", "/v1/quotes/"+id+"/accept", map[string]any{}, token); c != 200 {
			t.Fatal(c, r)
		}
		return id
	}
	sendBody := func(quote, key, message string) map[string]any {
		return map[string]any{"quote_id": quote, "idempotency_key": key, "class": "short", "message": message}
	}
	// open starts a streaming send and returns a reader over its SSE events plus a cancel (client disconnect).
	type live struct {
		events chan sseEvent
		cancel context.CancelFunc
		done   chan struct{}
	}
	open := func(token, course, quote, key, message string) *live {
		t.Helper()
		reqCtx, stop := context.WithCancel(ctx)
		raw, _ := json.Marshal(sendBody(quote, key, message))
		req, _ := http.NewRequestWithContext(reqCtx, "POST", server.URL+"/v1/courses/"+course+"/tutor/messages", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		l := &live{events: make(chan sseEvent, 64), cancel: stop, done: make(chan struct{})}
		go func() {
			defer close(l.done)
			defer close(l.events)
			resp, err := server.Client().Do(req)
			if err != nil {
				return
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				raw, _ := io.ReadAll(resp.Body)
				l.events <- sseEvent{Name: "status", Data: map[string]any{"code": float64(resp.StatusCode), "body": string(raw)}}
				return
			}
			buf := make([]byte, 0, 4096)
			chunk := make([]byte, 1024)
			for {
				n, err := resp.Body.Read(chunk)
				buf = append(buf, chunk[:n]...)
				for {
					i := bytes.Index(buf, []byte("\n\n"))
					if i < 0 {
						break
					}
					for _, ev := range parseSSE(string(buf[:i+2])) {
						l.events <- ev
					}
					buf = buf[i+2:]
				}
				if err != nil {
					return
				}
			}
		}()
		return l
	}
	await := func(l *live, name string) sseEvent {
		t.Helper()
		timeout := time.After(10 * time.Second)
		for {
			select {
			case ev, ok := <-l.events:
				if !ok {
					t.Fatalf("stream ended before %q", name)
				}
				if ev.Name == name {
					return ev
				}
			case <-timeout:
				t.Fatalf("timed out waiting for %q", name)
			}
		}
	}
	wait := func(what string, cond func() bool) {
		t.Helper()
		for range 200 {
			if cond() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("timed out: %s", what)
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.Pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	job := func(quote string) (string, int) {
		t.Helper()
		var state string
		var spent int
		if err := db.Pool.QueryRow(ctx, `SELECT state,COALESCE((result->>'content_spent')::int,0) FROM credit_jobs WHERE quote_id::text=$1`, quote).Scan(&state, &spent); err != nil {
			t.Fatal(err)
		}
		return state, spent
	}
	// settle the daily grant first so a before/after balance compares like with like.
	grant := func(owner string) {
		t.Helper()
		if _, err := financial.EnsureDailyGrant(ctx, owner, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	balance := func(owner string) int {
		return count(`SELECT COALESCE(sum(amount),0)::int FROM credit_entries WHERE learner_id=$1`, owner)
	}
	stopPath := func(course, key string) string { return "/v1/courses/" + course + "/tutor/turns/" + key + "/stop" }
	turnPath := func(course, key string) string { return "/v1/courses/" + course + "/tutor/turns/" + key }
	assistant := func(owner string) (content, status string, n int) {
		n = count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1 AND role='assistant'`, owner)
		_ = db.Pool.QueryRow(ctx, `SELECT content,status FROM tutor_messages WHERE learner_id=$1 AND role='assistant' ORDER BY seq DESC LIMIT 1`, owner).Scan(&content, &status)
		return
	}

	t.Run("explicit stop cancels the engine, stores one stopped prefix and settles once", func(t *testing.T) {
		token, owner := login("stop-a@example.test")
		fund(owner, 100)
		course := seedCourse(owner, "Stop")
		quote := accepted(token, course, "hold please")
		l := open(token, course, quote, "stop-1", "hold please")
		await(l, "delta")
		code, raw := call("POST", stopPath(course, "stop-1"), map[string]any{}, token)
		body := asMap(raw)
		if code != 200 || body["state"] != "stopped" || body["stopped"] != true || body["message_id"] == "" {
			t.Fatalf("stop: %d %s", code, raw)
		}
		l.cancel()
		<-l.done
		if content, status, n := assistant(owner); n != 1 || status != "stopped" || !strings.HasPrefix(content, "Moons") {
			t.Fatalf("one stopped prefix: %q %q %d", content, status, n)
		}
		state, spent := job(quote)
		if state != "settled" || spent == 0 {
			t.Fatalf("partial settlement: %s %d", state, spent)
		}
		wait("engine saw cancellation", func() bool { return fixture.cancelled.Load() >= 1 })
		if n := len(events.named("tutor_reply_stopped")); n != 1 {
			t.Fatalf("tutor_reply_stopped: %d", n)
		}
		stopped := events.named("tutor_reply_stopped")[0]
		if stopped["reason"] != "stop" || stopped["course_id"] != course {
			t.Fatalf("stopped properties: %v", stopped)
		}
		for _, e := range events.events {
			for k, v := range e {
				if s, ok := v.(string); ok && strings.Contains(s, "Moons") {
					t.Fatalf("reply text leaked into analytics %s.%s", e["_event"], k)
				}
			}
		}
		// Idempotent: a second stop is a no-op and cannot settle or charge again.
		before := balance(owner)
		code, raw = call("POST", stopPath(course, "stop-1"), map[string]any{}, token)
		if code != 200 || asMap(raw)["stopped"] != false || asMap(raw)["state"] != "stopped" {
			t.Fatalf("second stop: %d %s", code, raw)
		}
		if st2, sp2 := job(quote); st2 != state || sp2 != spent || balance(owner) != before || count(`SELECT count(*) FROM credit_jobs WHERE quote_id::text=$1`, quote) != 1 {
			t.Fatal("second stop changed settlement")
		}
		// The owner can read the stored partial after the connection is gone.
		code, raw = call("GET", turnPath(course, "stop-1"), nil, token)
		msg, _ := asMap(raw)["message"].(map[string]any)
		if code != 200 || asMap(raw)["state"] != "stopped" || msg["status"] != "stopped" || !strings.HasPrefix(fmt.Sprint(msg["content"]), "Moons") {
			t.Fatalf("recover partial: %d %s", code, raw)
		}
		// Retry after a stop is a new send with a fresh quote and key: never a replay.
		calls := fixture.calls.Load()
		quote2 := accepted(token, course, "quick again")
		l2 := open(token, course, quote2, "stop-2", "quick again")
		await(l2, "done")
		if fixture.calls.Load() != calls+1 || count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1 AND role='assistant'`, owner) != 2 {
			t.Fatal("retry must be a fresh, separately charged turn")
		}
	})

	t.Run("another learner cannot stop or read an in-flight reply", func(t *testing.T) {
		token, owner := login("stop-owner@example.test")
		other, otherOwner := login("stop-other@example.test")
		fund(owner, 100)
		otherCourse := seedCourse(otherOwner, "Other")
		course := seedCourse(owner, "Mine")
		quote := accepted(token, course, "hold mine")
		l := open(token, course, quote, "shared-key", "hold mine")
		await(l, "delta")
		for _, c := range []string{course, otherCourse} {
			if code, _ := call("POST", stopPath(c, "shared-key"), map[string]any{}, other); code != 404 {
				t.Fatalf("foreign stop on %s: %d", c, code)
			}
			if code, raw := call("GET", turnPath(c, "shared-key"), nil, other); code != 404 || strings.Contains(raw, "Moons") {
				t.Fatalf("foreign read on %s: %d %s", c, code, raw)
			}
		}
		if code, _ := call("POST", stopPath(course, "shared-key"), map[string]any{}, ""); code != 401 {
			t.Fatalf("anonymous stop: %d", code)
		}
		if fixture.cancelled.Load() != 0 && count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1`, owner) != 0 {
			t.Fatal("foreign stop reached the stream")
		}
		select {
		case <-l.done:
			t.Fatal("the owner's stream ended after a foreign stop")
		case <-time.After(300 * time.Millisecond):
		}
		if code, _ := call("POST", stopPath(course, "shared-key"), map[string]any{}, token); code != 200 {
			t.Fatalf("owner stop: %d", code)
		}
		l.cancel()
		<-l.done
	})

	t.Run("stop before the first token stores nothing and releases the whole reservation", func(t *testing.T) {
		token, owner := login("stop-silent@example.test")
		fund(owner, 100)
		course := seedCourse(owner, "Silent")
		grant(owner)
		before := balance(owner)
		quote := accepted(token, course, "hold-silent")
		l := open(token, course, quote, "silent-1", "hold-silent")
		wait("engine call", func() bool { return fixture.calls.Load() > 0 })
		time.Sleep(100 * time.Millisecond)
		code, raw := call("POST", stopPath(course, "silent-1"), map[string]any{}, token)
		if code != 200 || asMap(raw)["state"] != "released" {
			t.Fatalf("stop before first token: %d %s", code, raw)
		}
		l.cancel()
		<-l.done
		wait("tombstone", func() bool {
			return count(`SELECT count(*) FROM tutor_turns WHERE learner_id=$1 AND idempotency_key='silent-1' AND state='released' AND end_reason='stop'`, owner) == 1
		})
		// Repeated and late stops by the owner stay idempotent; unknown and foreign keys are still 404.
		for range 2 {
			if code, raw := call("POST", stopPath(course, "silent-1"), map[string]any{}, token); code != 200 || asMap(raw)["state"] != "released" || asMap(raw)["stopped"] != false {
				t.Fatalf("repeat stop after release: %d %s", code, raw)
			}
		}
		other, _ := login("silent-other@example.test")
		if code, _ := call("POST", stopPath(course, "silent-1"), map[string]any{}, other); code != 404 {
			t.Fatalf("foreign stop of a released key: %d", code)
		}
		if code, _ := call("POST", stopPath(course, "no-such-key"), map[string]any{}, token); code != 404 {
			t.Fatalf("unknown key: %d", code)
		}
		// The same key is never a second send: the learner uses a new key and quote.
		calls := fixture.calls.Load()
		if code, raw := call("POST", "/v1/courses/"+course+"/tutor/messages", sendBody(quote, "silent-1", "hold-silent"), token); code != 409 || asMap(raw)["error"] != "released" || fixture.calls.Load() != calls {
			t.Fatalf("resend of a released key: %d %s", code, raw)
		}
		state, spent := job(quote)
		if _, _, n := assistant(owner); n != 0 || state != "released" || spent != 0 || balance(owner) != before || count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1`, owner) != 0 {
			t.Fatalf("no charge without delivered content: state=%s spent=%d balance %d->%d", state, spent, before, balance(owner))
		}
	})

	t.Run("client disconnect settles the delivered prefix once as interrupted", func(t *testing.T) {
		token, owner := login("disconnect@example.test")
		fund(owner, 100)
		course := seedCourse(owner, "Disconnect")
		quote := accepted(token, course, "hold link")
		l := open(token, course, quote, "disc-1", "hold link")
		await(l, "delta")
		l.cancel()
		<-l.done
		wait("interrupted turn", func() bool {
			return count(`SELECT count(*) FROM tutor_turns WHERE learner_id=$1 AND state='interrupted' AND end_reason='disconnect'`, owner) == 1
		})
		if content, status, n := assistant(owner); n != 1 || status != "interrupted" || !strings.HasPrefix(content, "Moons") {
			t.Fatalf("disconnect prefix: %q %q %d", content, status, n)
		}
		wait("settled", func() bool { s, _ := job(quote); return s == "settled" })
		wait("stopped event", func() bool { return len(events.named("tutor_reply_stopped")) > 0 })
		last := events.named("tutor_reply_stopped")
		if last[len(last)-1]["reason"] != "disconnect" {
			t.Fatalf("disconnect reason: %v", last)
		}
		// Recovery by the owner after the dropped connection; a stop now is a no-op.
		code, raw := call("GET", turnPath(course, "disc-1"), nil, token)
		if code != 200 || asMap(raw)["state"] != "interrupted" || asMap(raw)["end_reason"] != "disconnect" {
			t.Fatalf("recover: %d %s", code, raw)
		}
		if code, raw = call("POST", stopPath(course, "disc-1"), map[string]any{}, token); code != 200 || asMap(raw)["stopped"] != false {
			t.Fatalf("stop after disconnect: %d %s", code, raw)
		}
		if n := count(`SELECT count(*) FROM credit_jobs WHERE quote_id::text=$1`, quote); n != 1 {
			t.Fatalf("jobs %d", n)
		}
	})

	t.Run("typed idle timeout keeps the prefix, is retryable and settles the engine estimate once", func(t *testing.T) {
		token, owner := login("idle@example.test")
		fund(owner, 100)
		course := seedCourse(owner, "Idle")
		quote := accepted(token, course, "idle now")
		l := open(token, course, quote, "idle-1", "idle now")
		ev := await(l, "error")
		if ev.Data["code"] != "idle_timeout" || ev.Data["retryable"] != true || ev.Data["interrupted"] != true || ev.Data["message_id"] == nil {
			t.Fatalf("typed error: %v", ev.Data)
		}
		<-l.done
		if content, status, n := assistant(owner); n != 1 || status != "interrupted" || content != "Moons orbit " {
			t.Fatalf("idle prefix: %q %q %d", content, status, n)
		}
		if state, spent := job(quote); state != "settled" || spent == 0 {
			t.Fatalf("settle: %s %d", state, spent)
		}
		failed := events.named("tutor_reply_failed")
		got := failed[len(failed)-1]
		if got["error_code"] != "idle_timeout" || got["retryable"] != true || got["interrupted"] != true {
			t.Fatalf("failed event: %v", got)
		}
		var estimated bool
		if err := db.Pool.QueryRow(ctx, `SELECT usage_estimated FROM tutor_turns WHERE learner_id=$1 AND idempotency_key='idle-1'`, owner).Scan(&estimated); err != nil || !estimated {
			t.Fatalf("estimated usage must be flagged: %v %v", estimated, err)
		}
	})

	t.Run("first token failure releases in full and reports a typed retryable error", func(t *testing.T) {
		token, owner := login("first@example.test")
		fund(owner, 100)
		course := seedCourse(owner, "First")
		grant(owner)
		before := balance(owner)
		quote := accepted(token, course, "first-timeout")
		code, raw := call("POST", "/v1/courses/"+course+"/tutor/messages", sendBody(quote, "first-1", "first-timeout"), token)
		body := asMap(raw)
		if code != 502 || body["error"] != "first_token_timeout" || body["retryable"] != true {
			t.Fatalf("first token: %d %s", code, raw)
		}
		if state, spent := job(quote); state != "released" || spent != 0 || balance(owner) != before || count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1`, owner) != 0 {
			t.Fatalf("release: %s %d", state, spent)
		}
	})

	t.Run("fallback usage and a missing final usage frame are accounted once", func(t *testing.T) {
		token, owner := login("fallback@example.test")
		fund(owner, 100)
		course := seedCourse(owner, "Fallback")
		q1 := accepted(token, course, "fallback please")
		l := open(token, course, q1, "fb-1", "fallback please")
		await(l, "done")
		outbox := func(where string) int {
			return count(`SELECT count(*) FROM credit_outbox WHERE event='$ai_generation' AND learner_id=$1 AND `+where, owner)
		}
		if state, _ := job(q1); state != "settled" || outbox(`properties->>'fallback_used'='true' AND properties->>'feature'='tutor_short' AND properties->>'usage_estimated'='false'`) != 1 {
			t.Fatalf("fallback accounting: %s", state)
		}
		q2 := accepted(token, course, "no-usage please")
		l = open(token, course, q2, "nu-1", "no-usage please")
		await(l, "done")
		if state, spent := job(q2); state != "settled" || spent == 0 {
			t.Fatalf("missing usage frame: %s %d", state, spent)
		}
		var estimated bool
		_ = db.Pool.QueryRow(ctx, `SELECT usage_estimated FROM tutor_turns WHERE learner_id=$1 AND idempotency_key='nu-1'`, owner).Scan(&estimated)
		if !estimated || outbox(`properties->>'usage_estimated'='true' AND properties->>'fallback_used'='false'`) != 1 {
			t.Fatal("a reply without a usage frame must be settled with flagged estimated usage")
		}
	})

	t.Run("concurrent stops race completion and settle exactly once", func(t *testing.T) {
		token, owner := login("race-stop@example.test")
		fund(owner, 500)
		course := seedCourse(owner, "Race")
		for i := range 8 {
			key := fmt.Sprint("race-", i)
			message := "quick race"
			quote := accepted(token, course, message)
			calls := fixture.calls.Load()
			l := open(token, course, quote, key, message)
			wait("handler running", func() bool { return fixture.calls.Load() > calls })
			var wg sync.WaitGroup
			for range 3 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					time.Sleep(time.Duration(i*5) * time.Millisecond)
					if code, _ := call("POST", stopPath(course, key), map[string]any{}, token); code != 200 && code != 202 {
						t.Errorf("stop status %d", code)
					}
				}()
			}
			wg.Wait()
			l.cancel()
			<-l.done
			wait("terminal turn", func() bool {
				return count(`SELECT count(*) FROM tutor_turns WHERE learner_id=$1 AND idempotency_key=$2 AND state<>'pending'`, owner, key) == 1 ||
					count(`SELECT count(*) FROM tutor_turns WHERE learner_id=$1 AND idempotency_key=$2`, owner, key) == 0
			})
			if n := count(`SELECT count(*) FROM credit_jobs WHERE quote_id::text=$1`, quote); n != 1 {
				t.Fatalf("%s: jobs %d", key, n)
			}
			if n := count(`SELECT count(*) FROM tutor_messages m JOIN tutor_turns t ON t.assistant_message_id=m.id WHERE t.learner_id=$1 AND t.idempotency_key=$2`, owner, key); n > 1 {
				t.Fatalf("%s: %d assistant messages", key, n)
			}
			state, _ := job(quote)
			if state != "settled" && state != "released" {
				t.Fatalf("%s: job state %s", key, state)
			}
		}
		if n := count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1 AND role='user'`, owner); n != count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1 AND role='assistant'`, owner) {
			t.Fatalf("user/assistant pairs diverged: %d", n)
		}
	})

	t.Run("stop after completion is a no-op", func(t *testing.T) {
		token, owner := login("late-stop@example.test")
		fund(owner, 100)
		course := seedCourse(owner, "Late")
		quote := accepted(token, course, "plain answer")
		l := open(token, course, quote, "late-1", "plain answer")
		await(l, "done")
		<-l.done
		before, beforeBalance := owner, balance(owner)
		_ = before
		code, raw := call("POST", stopPath(course, "late-1"), map[string]any{}, token)
		if code != 200 || asMap(raw)["state"] != "completed" || asMap(raw)["stopped"] != false {
			t.Fatalf("late stop: %d %s", code, raw)
		}
		if _, status, n := assistant(owner); status != "complete" || n != 1 || balance(owner) != beforeBalance {
			t.Fatal("late stop changed a completed reply")
		}
		if code, _ := call("POST", stopPath(course, "never-existed"), map[string]any{}, token); code != 404 {
			t.Fatalf("unknown key: %d", code)
		}
	})
}
