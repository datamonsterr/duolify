//go:build integration

package chat_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

// streamWhileEngineIdle holds the Engine silent for 600 ms with a 100 ms heartbeat, sends one
// tutor message through wrap(handler) and returns the streamed body.
func streamWhileEngineIdle(t *testing.T, wrap func(http.Handler) http.Handler) string {
	t.Helper()
	t.Setenv("SSE_HEARTBEAT_INTERVAL", "100ms")
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
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
	rpc := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(rpc, new(tutorEngine))
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	engine, err := backend.New("http://"+lis.Addr().String(), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	events := new(recorder)
	financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, events)
	courseService := courses.New(courses.Options{Pool: db.Pool, Environment: "test", Financial: financial, Events: events})
	tutor := chat.New(chat.Options{Pool: db.Pool, Engine: engine, Financial: financial, Profiles: courseService, Events: events})
	server := httptest.NewServer(wrap(httpapi.New(httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), Credits: financial, Tutor: tutor}, slog.New(slog.NewTextHandler(io.Discard, nil)))))
	defer server.Close()

	call := func(path string, body any, token string) *http.Response {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return resp
	}
	decode := func(path string, body any, token string) map[string]any {
		resp := call(path, body, token)
		defer func() { _ = resp.Body.Close() }()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return m
	}
	email := "sse-hb@example.test"
	_ = call("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, "").Body.Close()
	tokens := decode("/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)}, "")
	token := tokens["access_token"].(string)
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	owner := claims.Subject
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Pool.Exec(ctx, q, args...); e != nil {
			t.Fatalf("%v: %s", e, q)
		}
	}
	exec(`INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,100,'purchase','fund-hb',$2)`, owner, cat.Config().Version)
	var quote, creation, course string
	scan := func(dest *string, q string, args ...any) {
		t.Helper()
		if e := db.Pool.QueryRow(ctx, q, args...).Scan(dest); e != nil {
			t.Fatalf("%v: %s", e, q)
		}
	}
	scan(&quote, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) VALUES($1::uuid,'seed-ref',$2,'{}',now(),now()+interval '10 minutes') RETURNING id::text`, owner, cat.Config().Version)
	scan(&creation, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1::uuid,gen_random_uuid(),1,$2::uuid,'{}','enrolled') RETURNING id::text`, owner, quote)
	scan(&course, `WITH c AS (INSERT INTO courses(learner_id,creation_id,title,description) VALUES($1::uuid,$2::uuid,'Astronomy','Sky course') RETURNING id),
	 u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,c.id,'u1',0,'Sky' FROM c RETURNING id),
	 e AS (INSERT INTO course_enrollments(learner_id,course_id) SELECT $1::uuid,c.id FROM c)
	 SELECT id::text FROM c`, owner, creation)

	msg := "stall please"
	q := decode("/v1/courses/"+course+"/tutor/quote", map[string]any{"class": "short", "message": msg}, token)
	id := q["id"].(string)
	_ = call("/v1/quotes/"+id+"/accept", map[string]any{}, token).Body.Close()
	resp := call("/v1/courses/"+course+"/tutor/messages", map[string]any{"quote_id": id, "idempotency_key": "hb-1", "class": "short", "message": msg}, token)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(bufio.NewReader(resp.Body))
	return string(raw)
}

// TestTutorStreamHeartbeatWhileEngineIsIdleIntegration expects ":" comment lines before the first
// delta, then the normal reply.
func TestTutorStreamHeartbeatWhileEngineIsIdleIntegration(t *testing.T) {
	body := streamWhileEngineIdle(t, func(h http.Handler) http.Handler { return h })
	if !strings.Contains(body, "\n:") && !strings.HasPrefix(body, ":") {
		t.Fatalf("no heartbeat comment in idle stream: %q", body)
	}
	evs := parseSSE(body)
	var names []string
	for _, e := range evs {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "thread,delta,delta,delta,done" {
		t.Fatalf("heartbeats must not disturb event framing: %v", names)
	}
}

// brokenBeatWriter fails every keepalive write, as a dropped proxy connection would.
type brokenBeatWriter struct {
	http.ResponseWriter
	beats *atomic.Int32
}

func (w brokenBeatWriter) Write(b []byte) (int, error) {
	if strings.HasPrefix(string(b), ": keepalive") {
		w.beats.Add(1)
		return 0, errors.New("broken pipe")
	}
	return w.ResponseWriter.Write(b)
}

func (w brokenBeatWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// TestTutorStreamStopsAfterHeartbeatWriteErrorIntegration: a failed heartbeat write ends the loop
// at once instead of ticking on for the rest of the 600 ms stall (courses and lessontest do the same).
func TestTutorStreamStopsAfterHeartbeatWriteErrorIntegration(t *testing.T) {
	var beats atomic.Int32
	_ = streamWhileEngineIdle(t, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(brokenBeatWriter{ResponseWriter: w, beats: &beats}, r)
		})
	})
	if got := beats.Load(); got != 1 {
		t.Fatalf("heartbeat writes after the first failure: got %d attempts, want 1", got)
	}
}
