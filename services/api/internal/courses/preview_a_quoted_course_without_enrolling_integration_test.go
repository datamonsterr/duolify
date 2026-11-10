//go:build integration

package courses_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
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
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	backend "github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
)

type previewEngine struct {
	enginev1.UnimplementedEngineServiceServer
	calls   int
	last    *enginev1.PreviewCourseRequest
	started chan struct{}
	resume  chan struct{}
}

func (e *previewEngine) PreviewCourse(ctx context.Context, r *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	e.calls++
	e.last = r
	if r.Topic == "concurrent" {
		close(e.started)
		select {
		case <-e.resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	response := &enginev1.PreviewCourseResponse{SampleOutline: &enginev1.CourseOutline{Title: "Sample", Units: []*enginev1.Unit{{Title: "Unit", Lessons: []*enginev1.LessonRef{{Title: "Lesson", Goal: "Learn"}}}}}, ExampleSlides: []*enginev1.Slide{{SlideId: "sample", Html: `<div id="root">Example</div>`, Narration: "Example", DurationMs: 1000}}, Attempts: []*enginev1.Usage{{Provider: r.Limits.Provider, Model: r.Limits.Model, Attempt: 1, Component: "content", Delivered: true, InputTokens: 100, OutputTokens: 20}}}
	if r.Topic == "malformed" {
		response.SampleOutline.Title = ""
	}
	if r.Topic == "missing-usage" {
		response.Attempts = nil
	}
	if r.Topic == "outage" {
		response.Unavailable = true
		response.SampleOutline = nil
		response.Attempts[0].Delivered = false
	}
	return response, nil
}

// Delays in quote-bound resolution must consume the original reservation budget.
type delayedPreviewFinancial struct{ *credits.Service }

func (f delayedPreviewFinancial) Limits(ctx context.Context, owner, id, action string) (*enginev1.UsageLimits, error) {
	select {
	case <-time.After(40 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return f.Service.Limits(ctx, owner, id, action)
}
func TestQuotedPreviewIntegration(t *testing.T) {
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
	fixture := &previewEngine{started: make(chan struct{}), resume: make(chan struct{})}
	rpc := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(rpc, fixture)
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	engine, err := backend.New("http://"+lis.Addr().String(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key", 5), mail)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}

	var captured []map[string]any
	var eventsMu sync.Mutex
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		eventsMu.Lock()
		captured = append(captured, body.Batch...)
		eventsMu.Unlock()
		w.WriteHeader(200)
	}))
	defer sink.Close()
	events, err := analytics.New("fixture-only", sink.URL, func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()
	financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, events)
	svc := courses.New(courses.Options{Pool: db.Pool, Events: events})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Engine: engine, CourseBrief: svc, Quotes: pricing.NewQuotes(db.Pool), Credits: delayedPreviewFinancial{financial}}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	call := func(method, path string, body any, token string, quoteID ...string) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		if len(quoteID) > 0 {
			r.Header.Set("X-Quote-ID", quoteID[0])
		}
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

	token := login("preview@example.test")
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	owner := claims.Subject
	confirm := func(topic string) string {
		t.Helper()
		code, out := call("POST", "/v1/course-brief/confirm", map[string]any{"brief": courses.Brief{Topic: topic, IncludeTopics: "Moon", SkipTopics: "History", Maths: "No maths", Language: "vi"}, "confirmed": true}, token)
		if code != 201 {
			t.Fatalf("confirm %d %v", code, out)
		}
		return out["id"].(string)
	}
	quote := func(revision string) string {
		t.Helper()
		code, out := call("POST", "/v1/quotes", map[string]any{"action": "preview", "brief_revision": revision}, token)
		if code != 200 {
			t.Fatalf("quote %d %v", code, out)
		}
		id := out["id"].(string)
		if code, _ = call("POST", "/v1/quotes/"+id+"/accept", map[string]any{}, token); code != 200 {
			t.Fatal(code)
		}
		return id
	}
	preview := func(revision, q string) int {
		code, _ := call("POST", "/v1/courses/preview", map[string]string{"brief_revision": revision}, token, q)
		return code
	}
	first := confirm("Astronomy")
	q := quote(first)
	if code := preview(first, q); code != 409 || fixture.calls != 0 {
		t.Fatalf("insufficient %d calls %d", code, fixture.calls)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,400,'purchase','preview-fixture',$2)`, owner, cat.Config().Version); err != nil {
		t.Fatal(err)
	}
	second := confirm("New topic")
	if code := preview(second, q); code != 409 || fixture.calls != 0 {
		t.Fatalf("stale %d calls %d", code, fixture.calls)
	}
	q = quote(second)
	if code := preview(second, q); code != 200 {
		t.Fatalf("preview %d", code)
	}
	var jobDeadline time.Time
	if err = db.Pool.QueryRow(ctx, "SELECT deadline_at FROM credit_jobs WHERE quote_id::text=$1", q).Scan(&jobDeadline); err != nil {
		t.Fatal(err)
	}
	if fixture.last.Limits.DeadlineUnixMs != uint64(jobDeadline.UnixMilli()) {
		t.Errorf("provider absolute deadline %v differs from reserved deadline %v", fixture.last.Limits.DeadlineUnixMs, jobDeadline)
	}
	if fixture.last.BriefRevision != second || fixture.last.Brief.IncludeTopics != "Moon" || fixture.last.Brief.SkipTopics != "History" || fixture.last.Brief.Maths != "No maths" || fixture.last.Learner.LearnerId != owner {
		t.Fatalf("lost confirmed brief/profile %v", fixture.last)
	}
	if code := preview(second, q); code != 409 || fixture.calls != 1 {
		t.Fatalf("duplicate %d calls %d", code, fixture.calls)
	}
	for _, topic := range []string{"malformed", "outage", "missing-usage"} {
		revision := confirm(topic)
		id := quote(revision)
		if code := preview(revision, id); code != 502 {
			t.Fatalf("%s %d", topic, code)
		}
		var state string
		var charged int64
		if err = db.Pool.QueryRow(ctx, `SELECT state,COALESCE((result->>'content_spent')::bigint,0) FROM credit_jobs WHERE quote_id::text=$1`, id).Scan(&state, &charged); err != nil {
			t.Fatal(err)
		}
		if state != "released" || charged != 0 {
			t.Fatalf("failed not released %s %d", state, charged)
		}
		if code, _ := call("GET", "/v1/course-brief/revisions/"+revision, nil, token); code != 200 {
			t.Fatal("brief lost")
		}
	}

	// Two HTTP executors must never release each other's running reservation.
	concurrentRevision := confirm("concurrent")
	concurrentQuote := quote(concurrentRevision)
	completed := make(chan int, 1)
	go func() { completed <- preview(concurrentRevision, concurrentQuote) }()
	select {
	case <-fixture.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if code := preview(concurrentRevision, concurrentQuote); code != 409 {
		t.Fatalf("duplicate running %d", code)
	}
	var running string
	if err = db.Pool.QueryRow(ctx, "SELECT state FROM credit_jobs WHERE quote_id::text=$1", concurrentQuote).Scan(&running); err != nil || running != "running" {
		t.Fatalf("duplicate canceled original: %s %v", running, err)
	}
	close(fixture.resume)
	if code := <-completed; code != 200 {
		t.Fatal(code)
	}
	if err = financial.DispatchOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	// Current profile opt-out suppresses both course and usage emitters.
	if _, err = db.Pool.Exec(ctx, "UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id::text=$1", owner); err != nil {
		t.Fatal(err)
	}
	privateRevision := confirm("private-topic")
	privateQuote := quote(privateRevision)
	if code := preview(privateRevision, privateQuote); code != 200 {
		t.Fatal(code)
	}
	if err = financial.DispatchOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	counts := map[string]int{}
	for _, event := range captured {
		name, _ := event["event"].(string)
		counts[name]++
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "private-topic") || strings.Contains(string(raw), "History") || strings.Contains(string(raw), "Moon") {
			t.Fatalf("private event %s", raw)
		}
		if event["distinct_id"] != owner {
			t.Fatalf("wrong learner %v", event)
		}
	}
	if counts["course_preview_requested"] != 5 || counts["course_preview_viewed"] != 2 || counts["course_generation_failed"] != 3 || counts["$ai_generation"] != 5 {
		t.Fatalf("course event delivery/optout %v", counts)
	}

	for _, table := range []struct{ name, query string }{{"courses", "SELECT count(*) FROM courses"}, {"enrollments", "SELECT count(*) FROM enrollments"}, {"course_enrollments", "SELECT count(*) FROM course_enrollments"}} {
		var exists bool
		if err = db.Pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table.name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			var count int
			if err = db.Pool.QueryRow(ctx, table.query).Scan(&count); err != nil || count != 0 {
				t.Fatalf("preview persisted %s: %d %v", table.name, count, err)
			}
		}
	}
}
