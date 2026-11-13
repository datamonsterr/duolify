//go:build integration

package courses_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/accountstest"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	backend "github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type briefEngine struct {
	enginev1.UnimplementedEngineServiceServer
}

func (briefEngine) CourseBriefChat(_ context.Context, r *enginev1.CourseBriefChatRequest) (*enginev1.CourseBriefChatResponse, error) {
	if r.Answer == "outage" {
		return nil, status.Error(codes.Unavailable, "mock outage")
	}
	if r.Answer == "failed-model" {
		return &enginev1.CourseBriefChatResponse{Unavailable: true, Attempts: []*enginev1.BriefAttempt{
			{Provider: "mock", Model: "fixture"}, {Provider: "mock", Model: "fixture"}, {Provider: "mock", Model: "fixture", FallbackUsed: true},
		}}, nil
	}
	b := r.GetBrief()
	b.Topic = r.Answer
	return &enginev1.CourseBriefChatResponse{Brief: b, Message: "What is your goal?", NextStep: "goal", Attempts: []*enginev1.BriefAttempt{{Usage: &enginev1.Usage{Provider: "mock", Model: "fixture", InputTokens: 10, OutputTokens: 5}}}}, nil
}
func TestCourseBriefIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("brief"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	lis, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(rpc, briefEngine{})
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	engine, err := backend.New("http://"+lis.Addr().String(), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key", 5), mail)
	if err != nil {
		t.Fatal(err)
	}
	ledger := accountstest.NewDeletionLedger()
	auth.ConfigureDeletionLedger(ledger)
	cat, err := pricing.LoadDevelopment(ctx, db.Pool)
	if err != nil {
		t.Fatal(err)
	}
	var captured []map[string]any
	var eventsMu sync.Mutex
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Error(err)
		}
		eventsMu.Lock()
		captured = append(captured, batch.Batch...)
		eventsMu.Unlock()
		w.WriteHeader(200)
	}))
	defer sink.Close()
	events, err := analytics.New("test-only", sink.URL, func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	svc := courses.New(courses.Options{Pool: db.Pool, Engine: engine, Pricing: cat, Environment: "development", Events: events})
	mux := http.NewServeMux()
	mux.Handle("/v1/auth/", auth)
	mux.Handle("/v1/me/", auth)
	mux.Handle("/v1/course-brief/", auth.Protect(svc))
	server := httptest.NewServer(mux)
	defer server.Close()
	call := func(method, path string, body any, token string) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	login := func(email string) string {
		code, _ := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true, "locale": "en", "time_zone": "UTC"}, "")
		if code != 202 {
			t.Fatalf("request %d", code)
		}
		code, out := call("POST", "/v1/auth/verify", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true, "locale": "en", "time_zone": "UTC"}, "")
		if code != 200 {
			t.Fatalf("verify %d %v", code, out)
		}
		return out["access_token"].(string)
	}
	token := login("brief@example.test")
	other := login("other-brief@example.test")
	if code, _ := call("POST", "/v1/course-brief/chat", map[string]any{}, ""); code != 401 {
		t.Fatalf("anonymous %d", code)
	}
	if code, out := call("POST", "/v1/course-brief/chat", map[string]any{"current_step": "topic", "answer": "Astronomy", "brief": map[string]string{}}, token); code != 200 {
		t.Fatalf("chat %d %v", code, out)
	}
	var count int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM course_brief_revisions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("draft persisted %d %v", count, err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries").Scan(&count); err != nil || count != 0 {
		t.Fatalf("charged %d %v", count, err)
	}
	if code, _ := call("POST", "/v1/course-brief/confirm", map[string]any{"brief": map[string]string{"topic": "Astronomy"}}, token); code != 400 {
		t.Fatalf("missing consent %d", code)
	}
	code, out := call("POST", "/v1/course-brief/confirm", map[string]any{"confirmed": true, "brief": map[string]string{"topic": "Astronomy", "include_topics": "Moon", "skip_topics": "History", "maths": "Light maths"}}, token)
	if code != 201 {
		t.Fatalf("confirm %d %v", code, out)
	}
	id := out["id"].(string)
	if code, _ = call("GET", "/v1/course-brief/revisions/"+id, nil, other); code != 404 {
		t.Fatalf("ownership %d", code)
	}
	if code, out = call("GET", "/v1/course-brief/revisions/"+id, nil, token); code != 200 || out["content_hash"] == "" {
		t.Fatalf("read %d %v", code, out)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE course_brief_revisions SET brief='{}' WHERE id=$1::uuid", id); err == nil {
		t.Fatal("revision mutable")
	}
	if code, _ = call("POST", "/v1/course-brief/chat", map[string]any{"current_step": "goal", "answer": "outage", "brief": map[string]string{"topic": "Astronomy"}}, token); code != 503 {
		t.Fatalf("outage %d", code)
	}
	if code, _ = call("POST", "/v1/course-brief/chat", map[string]any{"current_step": "goal", "answer": "failed-model", "brief": map[string]string{}}, token); code != 503 {
		t.Fatalf("model failure %d", code)
	}
	code, out = call("GET", "/v1/me/data-export", nil, token)
	if code != 200 || !strings.Contains(string(mustJSON(out)), "course_brief_revisions") {
		t.Fatalf("export %d %v", code, out)
	}
	// Reads enforce expiry even before nightly physical cleanup.
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	var expiredID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_brief_revisions(learner_id,brief,content_hash,expires_at) VALUES($1::uuid,'{"topic":"Expired"}',repeat('e',64),now()-interval '1 second') RETURNING id::text`, claims.Subject).Scan(&expiredID); err != nil {
		t.Fatal(err)
	}
	if code, _ = call("GET", "/v1/course-brief/revisions/"+expiredID, nil, token); code != 404 {
		t.Fatalf("expired %d", code)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1::uuid`, claims.Subject); err != nil {
		t.Fatal(err)
	}
	if code, _ = call("POST", "/v1/course-brief/chat", map[string]any{"current_step": "topic", "answer": "Private opted-out answer", "brief": map[string]string{}}, token); code != 200 {
		t.Fatalf("opt out %d", code)
	}
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	eventsMu.Lock()
	if len(captured) != 4 {
		t.Fatalf("expected opted-in success plus three failed model attempts, got %d", len(captured))
	}
	for index, e := range captured {
		if e["distinct_id"] != claims.Subject || e["event"] != "$ai_generation" {
			t.Fatalf("event identity %v", e)
		}
		props := e["properties"].(map[string]any)
		if props["source"] != "api" || props["platform"] != "web" || props["app_version"] == "" || props["task"] != "course_brief_chat" {
			t.Fatalf("event properties %v", props)
		}
		if index == 0 && (props["input_tokens"] != float64(10) || props["usage_available"] != true) {
			t.Fatalf("successful usage %v", props)
		}
		if index > 0 && (props["input_tokens"] != float64(0) || props["usage_available"] != false || props["provider"] != "mock" || props["model"] != "fixture") {
			t.Fatalf("unknown usage must retain route without inventing tokens: %v", props)
		}
		raw := string(mustJSON(e))
		for _, private := range []string{"Astronomy", "Private opted-out answer", "Moon", "History"} {
			if strings.Contains(raw, private) {
				t.Fatalf("private analytics %s", private)
			}
		}
	}
	eventsMu.Unlock()
	// Persisted platform limits are shared by replicas and do not touch the ledger.
	config := cat.Config()
	var limits map[string]any
	_ = json.Unmarshal(config.Limits, &limits)
	limits["course_brief_chat"].(map[string]any)["requests_per_hour"] = 1
	config.Limits = mustJSON(limits)
	limited := courses.New(courses.Options{Pool: db.Pool, Engine: engine, Pricing: pricing.FromConfig(config), Environment: "development"})
	limitedReq := authorizedRequest(t, token, "/v1/course-brief/chat")
	limitedReq.Body = io.NopCloser(strings.NewReader(`{"current_step":"topic","answer":"Limited","brief":{}}`))
	limitedW := httptest.NewRecorder()
	auth.Protect(limited).ServeHTTP(limitedW, limitedReq)
	if limitedW.Code != 429 {
		t.Fatalf("shared rate limit %d %s", limitedW.Code, limitedW.Body)
	}
	production := courses.New(courses.Options{Pool: db.Pool, Engine: engine, Pricing: cat, Environment: "production"})
	w := httptest.NewRecorder()
	auth.Protect(production).ServeHTTP(w, authorizedRequest(t, token, "/v1/course-brief/chat"))
	if w.Code != 503 {
		t.Fatalf("production %d", w.Code)
	}
	if code, _ = call("POST", "/v1/me/deletion-request", map[string]string{"confirmation": "DELETE"}, token); code != 200 {
		t.Fatalf("delete %d", code)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM course_brief_revisions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("erasure %d %v", count, err)
	}
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func authorizedRequest(t *testing.T, token, path string) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), "POST", path, strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}
