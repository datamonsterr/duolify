//go:build integration

package courses_test

import (
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
	"sync/atomic"

	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
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

type outlineProposalEngine struct {
	enginev1.UnimplementedEngineServiceServer
	calls   atomic.Int32
	last    *enginev1.OutlineChatChangeRequest
	started chan struct{}
	resume  chan struct{}
}

func (e *outlineProposalEngine) OutlineChatChange(ctx context.Context, r *enginev1.OutlineChatChangeRequest) (*enginev1.OutlineChatChangeResponse, error) {
	e.calls.Add(1)
	e.last = r
	if r.Request == "concurrent" {
		close(e.started)
		select {
		case <-e.resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	result := &enginev1.OutlineChatChangeResponse{Operations: []*enginev1.OutlineOperation{{Kind: "retitled", Id: "u1", Title: "Suggested"}}, Attempts: []*enginev1.Usage{{Provider: r.Limits.Provider, Model: r.Limits.Model, Attempt: 1, Component: "content", Delivered: true, InputTokens: 100, OutputTokens: 20}}}
	if r.Request == "invalid" {
		result.Operations[0].Id = "missing"
	}
	if r.Request == "outage" {
		result.Unavailable = true
	}
	if r.Request == "missing usage" {
		result.Attempts = nil
	}
	if r.Request == "bad usage" {
		result.Attempts[0].InputTokens = 999999
	}
	return result, nil
}

func TestOutlineChatChangeIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("outline"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	fixture := new(outlineProposalEngine)
	rpc := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(rpc, fixture)
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	engine, err := backend.New("http://"+lis.Addr().String(), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	var captured []map[string]any
	var mu sync.Mutex
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Error(err)
		}
		mu.Lock()
		captured = append(captured, batch.Batch...)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer sink.Close()
	events, err := analytics.New("fixture-only", sink.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, events)
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), Credits: financial, CourseBrief: courses.New(courses.Options{Pool: db.Pool, Environment: "test", ProposalEngine: engine, Financial: financial, Events: events})}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	call := func(method, path string, body any, token string) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	login := func(email string) string {
		if code, _ := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); code != 202 {
			t.Fatal(code)
		}
		code, out := call("POST", "/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)}, "")
		if code != 200 {
			t.Fatal(code, out)
		}
		return out["access_token"].(string)
	}
	token := login("outline@example.test")
	other := login("outline-other@example.test")
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	owner := claims.Subject
	code, brief := call("POST", "/v1/course-brief/confirm", map[string]any{"confirmed": true, "brief": map[string]string{"topic": "Sky"}}, token)
	if code != 201 {
		t.Fatal(code, brief)
	}

	outline := courses.Outline{{ID: "u1", Title: "My manual title", Lessons: []courses.OutlineLesson{{ID: "l1", Title: "Moon", Goal: "Observe"}}}}
	code, draft := call("POST", "/v1/outline-drafts", map[string]any{"brief_revision": brief["id"], "outline": outline}, token)
	if code != 201 {
		t.Fatal(code, draft)
	}
	code, quote := call("POST", "/v1/outline-proposals/quote", map[string]any{"draft_id": draft["id"], "expected_revision": 1, "request": "Put planets first"}, token)
	if code != 200 || quote["daily_free_eligible"] != true || quote["pricing_version"] == "" {
		t.Fatalf("configured proposal quote: %d %v", code, quote)
	}
	quoted := func(request string) map[string]any {
		t.Helper()
		code, q := call("POST", "/v1/outline-proposals/quote", map[string]any{"draft_id": draft["id"], "expected_revision": draft["revision"], "request": request}, token)
		if code != 200 {
			t.Fatal(code, q)
		}
		if code, _ = call("POST", "/v1/quotes/"+q["id"].(string)+"/accept", map[string]any{}, token); code != 200 {
			t.Fatal(code)
		}
		return map[string]any{"draft_id": draft["id"], "expected_revision": draft["revision"], "request": request, "quote_id": q["id"]}
	}
	if _, err = financial.EnsureDailyGrant(ctx, owner, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version,provenance) SELECT $1,-COALESCE(sum(amount),0),'spend','drain-outline',$2,jsonb_build_object('bucket','daily_free','date',to_char(now() AT TIME ZONE 'UTC','YYYY-MM-DD')) FROM credit_entries WHERE learner_id=$1`, owner, cat.Config().Version); err != nil {
		t.Fatal(err)
	}
	if code, out := call("POST", "/v1/outline-proposals", quoted("insufficient"), token); code != 409 || fixture.calls.Load() != 0 {
		t.Fatal("insufficient must not call engine", code, out, fixture.calls.Load())
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,400,'purchase','fund-outline',$2)`, owner, cat.Config().Version); err != nil {
		t.Fatal(err)
	}
	request := quoted("Preserve private manual text")
	code, p := call("POST", "/v1/outline-proposals", request, token)
	if code != 200 || p["state"] != "delivered" {
		t.Fatal("delivery", code, p)
	}
	if p["base_outline"].([]any)[0].(map[string]any)["title"] != "My manual title" || p["request"] != "Preserve private manual text" {
		t.Fatal("immutable diff base lost", p)
	}
	if fixture.last.Outline.Units[0].Title != "My manual title" || fixture.last.Outline.Units[0].Lessons[0].LessonId != "l1" || fixture.last.BaseRevision != 1 {
		t.Fatal("edited proto lost", fixture.last)
	}
	if code, again := call("POST", "/v1/outline-proposals", request, token); code != 200 || again["id"] != p["id"] || fixture.calls.Load() != 1 {
		t.Fatal("duplicate generated", code, again)
	}
	path := "/v1/outline-proposals/" + p["id"].(string)
	if code, _ := call("GET", path, nil, other); code != 404 {
		t.Fatal("ownership", code)
	}
	if code, resumed := call("GET", path, nil, token); code != 200 || resumed["id"] != p["id"] {
		t.Fatal("resume", code, resumed)
	}
	if code, unchanged := call("GET", "/v1/outline-drafts/"+draft["id"].(string), nil, token); code != 200 || unchanged["revision"] != draft["revision"] {
		t.Fatal("delivery mutated draft", code, unchanged)
	}
	code, applied := call("POST", path+"/apply", nil, token)
	if code != 200 {
		t.Fatal("apply", code, applied)
	}
	draft = applied["draft"].(map[string]any)
	if draft["revision"] != float64(2) {
		t.Fatal(draft)
	}
	if code, duplicate := call("POST", path+"/apply", nil, token); code != 200 || duplicate["draft"].(map[string]any)["revision"] != float64(2) {
		t.Fatal("duplicate apply", code, duplicate)
	}
	code, undone := call("POST", path+"/undo", nil, token)
	if code != 200 {
		t.Fatal("undo", code, undone)
	}
	draft = undone["draft"].(map[string]any)
	if code, duplicate := call("POST", path+"/undo", nil, token); code != 200 || duplicate["draft"].(map[string]any)["revision"] != draft["revision"] {
		t.Fatal("duplicate undo", code, duplicate)
	}
	if draft["outline"].([]any)[0].(map[string]any)["title"] != "My manual title" {
		t.Fatal("undo lost manual", draft)
	}
	for _, action := range []string{"discard", "stale"} {
		code, p = call("POST", "/v1/outline-proposals", quoted(action), token)
		if code != 200 {
			t.Fatal(code, p)
		}
		path = "/v1/outline-proposals/" + p["id"].(string)
		if action == "stale" {
			code, edited := call("PUT", "/v1/outline-drafts/"+draft["id"].(string), map[string]any{"expected_revision": draft["revision"], "outline": outline}, token)
			if code != 200 {
				t.Fatal(code, edited)
			}
			draft = edited
			if code, out := call("POST", path+"/apply", nil, token); code != 409 {
				t.Fatal("stale overwritten", code, out)
			}
		}
		for range 2 {
			if code, out := call("POST", path+"/discard", nil, token); code != 200 {
				t.Fatal("discard", code, out)
			}
		}
	}
	for _, request := range []string{"invalid", "outage", "missing usage", "bad usage"} {
		in := quoted(request)
		code, out := call("POST", "/v1/outline-proposals", in, token)
		if code != 502 {
			t.Fatal("failure", request, code, out)
		}
		var state string
		var spent int
		if err = db.Pool.QueryRow(ctx, `SELECT state,COALESCE((result->>'content_spent')::int,0) FROM credit_jobs WHERE quote_id::text=$1`, in["quote_id"]).Scan(&state, &spent); err != nil || state != "released" || spent != 0 {
			t.Fatal("failed hold", request, state, spent, err)
		}
	}
	if err = financial.DispatchOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id::text=$1", owner); err != nil {
		t.Fatal(err)
	}
	if code, out := call("POST", "/v1/outline-proposals", quoted("opted out"), token); code != 200 {
		t.Fatal(code, out)
	}
	if err = financial.DispatchOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	counts := map[string]int{}
	for _, event := range captured {
		name, _ := event["event"].(string)
		counts[name]++
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "Preserve private") || strings.Contains(string(raw), "My manual title") || strings.Contains(string(raw), "Suggested") || event["distinct_id"] != owner {
			t.Fatal("private analytics", event)
		}
		props := event["properties"].(map[string]any)
		if props["source"] != "api" || props["platform"] != "web" || props["app_version"] == "" {
			t.Fatal("common properties", event)
		}
	}
	if counts["outline_proposal_delivered"] != 3 || counts["outline_proposal_applied"] != 1 || counts["outline_proposal_undone"] != 1 || counts["outline_proposal_discarded"] != 2 || counts["outline_proposal_failed"] != 4 || counts["$ai_generation"] != 7 {
		t.Fatal("delivery/optout", counts)
	}
	// A second executor only reads the first executor's durable proposal.
	fixture.started, fixture.resume = make(chan struct{}), make(chan struct{})
	concurrent := quoted("concurrent")
	before := fixture.calls.Load()
	completed := make(chan int, 1)
	go func() { code, _ := call("POST", "/v1/outline-proposals", concurrent, token); completed <- code }()
	select {
	case <-fixture.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	code, pending := call("POST", "/v1/outline-proposals", concurrent, token)
	if code != 200 || pending["state"] != "pending" || fixture.calls.Load() != before+1 {
		t.Fatal("duplicate executor", code, pending)
	}
	if code, resumed := call("GET", "/v1/outline-proposals/?quote_id="+concurrent["quote_id"].(string), nil, token); code != 200 || resumed["id"] != pending["id"] {
		t.Fatal("quote reconnect", code, resumed)
	}
	var running string
	if err = db.Pool.QueryRow(ctx, "SELECT state FROM credit_jobs WHERE quote_id::text=$1", concurrent["quote_id"]).Scan(&running); err != nil || running != "running" {
		t.Fatal("duplicate released original", running, err)
	}
	close(fixture.resume)
	if code = <-completed; code != 200 {
		t.Fatal("original delivery", code)
	}
	// Crash recovery must release once and never repeat generation, whether the
	// process stopped before reserve or after claiming its running reservation.
	for _, claimed := range []bool{false, true} {
		in := quoted("abandoned")
		raw, _ := json.Marshal(draft["outline"])
		var id string
		if err = db.Pool.QueryRow(ctx, `INSERT INTO outline_proposals(learner_id,draft_id,quote_id,base_revision,base_outline,request,created_at) VALUES($1,$2,$3,$4,$5,'abandoned',now()-interval '1 minute') RETURNING id::text`, owner, draft["id"], in["quote_id"], draft["revision"], raw).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if claimed {
			job, e := financial.ReserveAction(ctx, owner, in["quote_id"].(string), "outline_chat_change")
			if e != nil {
				t.Fatal(e)
			}
			if e = financial.Claim(ctx, owner, job.ID); e != nil {
				t.Fatal(e)
			}
			if _, e = db.Pool.Exec(ctx, "UPDATE credit_jobs SET deadline_at=clock_timestamp()-interval '1 second' WHERE id::text=$1", job.ID); e != nil {
				t.Fatal(e)
			}
		}
		before = fixture.calls.Load()
		for range 2 {
			if code, out := call("GET", "/v1/outline-proposals/"+id, nil, token); code != 200 || out["state"] != "failed" {
				t.Fatal("abandoned reconnect", code, out)
			}
		}
		if fixture.calls.Load() != before {
			t.Fatal("recovery called engine")
		}
		if claimed {
			if err = db.Pool.QueryRow(ctx, "SELECT state FROM credit_jobs WHERE quote_id::text=$1", in["quote_id"]).Scan(&running); err != nil || running != "released" {
				t.Fatal("abandoned hold", running, err)
			}
		}
	}
	code, export := call("GET", "/v1/me/data-export", nil, token)
	if code != 200 || len(export["tables"].(map[string]any)["outline_proposals"].([]any)) < 1 {
		t.Fatal("proposal export", code, export)
	}
	if code, out := call("DELETE", "/v1/outline-drafts/"+draft["id"].(string), nil, token); code != 204 {
		t.Fatal("draft deletion", code, out)
	}
	var remaining int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM outline_proposals WHERE learner_id::text=$1", owner).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("private proposal erasure", remaining, err)
	}
}
