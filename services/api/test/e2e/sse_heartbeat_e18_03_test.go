package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/chat"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	backend "github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"google.golang.org/grpc"
)

// stallEngine holds the chat reply until the HTTP client has seen a heartbeat.
type stallEngine struct {
	enginev1.UnimplementedEngineServiceServer
	release chan struct{}
}

func (e *stallEngine) Chat(r *enginev1.ChatRequest, stream enginev1.EngineService_ChatServer) error {
	select {
	case <-e.release:
	case <-time.After(30 * time.Second):
	}
	_ = stream.Send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_Delta{Delta: "hello"}})
	return stream.Send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_Usage{Usage: &enginev1.Usage{Provider: r.Limits.Provider, Model: r.Limits.Model, InputTokens: 10, OutputTokens: 1}}})
}

// TestChatStreamSendsHeartbeatWithin25SecondsOverHTTP keeps the Engine silent and requires a ":"
// comment line on the wire inside Cloudflare's budget, using the default heartbeat interval.
func TestChatStreamSendsHeartbeatWithin25SecondsOverHTTP(t *testing.T) {
	auth, token, db := signedInDatabase(t)
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	owner, ctx := claims.Subject, t.Context()
	lis, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &stallEngine{release: make(chan struct{})}
	rpc := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(rpc, fixture)
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	engine, err := backend.New("http://"+lis.Addr().String(), 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
	courseService := courses.New(courses.Options{Pool: db.Pool, Environment: "test", Financial: financial})
	tutor := chat.New(chat.Options{Pool: db.Pool, Engine: engine, Financial: financial, Profiles: courseService})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), Credits: financial, Tutor: tutor}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Pool.Exec(ctx, q, args...); e != nil {
			t.Fatalf("%v: %s", e, q)
		}
	}
	exec(`INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,100,'purchase','fund-e18-03',$2)`, owner, cat.Config().Version)
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

	post := func(ctx context.Context, path string, body any) *http.Response {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return resp
	}
	resp := post(ctx, "/v1/courses/"+course+"/tutor/quote", map[string]any{"class": "short", "message": "What is a moon?"})
	var q struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&q)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || q.ID == "" {
		t.Fatalf("quote: %d", resp.StatusCode)
	}
	resp = post(ctx, "/v1/quotes/"+q.ID+"/accept", map[string]any{})
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("accept: %d", resp.StatusCode)
	}
	// Headers must also arrive inside the budget: the first heartbeat commits them.
	streamCtx, stop := context.WithTimeout(ctx, 25*time.Second)
	defer stop()
	resp = post(streamCtx, "/v1/courses/"+course+"/tutor/messages", map[string]any{"quote_id": q.ID, "idempotency_key": "k1", "class": "short", "message": "What is a moon?"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("messages: %d", resp.StatusCode)
	}

	seen := make(chan struct{})
	var once sync.Once
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), ":") {
				once.Do(func() { close(seen) })
			}
		}
	}()
	select {
	case <-seen:
		close(fixture.release)
	case <-time.After(25 * time.Second):
		close(fixture.release)
		t.Fatal("no SSE heartbeat comment within 25s of an idle chat stream")
	}
}
