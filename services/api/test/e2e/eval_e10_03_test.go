package e2e

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/eval"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// providerDouble simulates primary and fallback LLM provider failure modes.
type providerDouble struct {
	enginev1.UnimplementedEngineServiceServer
	mu       sync.Mutex
	mode     string
	attempts []*enginev1.Usage
}

func (p *providerDouble) Health(context.Context, *enginev1.HealthRequest) (*enginev1.HealthResponse, error) {
	return &enginev1.HealthResponse{Version: "e10-03-test", Providers: []string{"anthropic", "openrouter"}}, nil
}

func (p *providerDouble) PreviewCourse(ctx context.Context, req *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	topic := req.GetTopic()
	switch {
	case strings.HasPrefix(topic, "429-fallback"):
		// Primary hit 429; fallback succeeded
		p.attempts = []*enginev1.Usage{
			{
				Provider:         req.Limits.Provider,
				Model:            req.Limits.Model,
				InputTokens:      0,
				OutputTokens:     0,
				Attempt:          1,
				FallbackUsed:     false,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: true,
				ErrorCode:        "rate_limited",
			},
			{
				Provider:         req.Limits.FallbackProvider,
				Model:            req.Limits.FallbackModel,
				InputTokens:      120,
				OutputTokens:     45,
				Attempt:          2,
				FallbackUsed:     true,
				Component:        "content",
				Delivered:        true,
				UsageUnavailable: false,
			},
		}
		return &enginev1.PreviewCourseResponse{
			Attempts: p.attempts,
			Usage:    p.attempts[1],
			SampleOutline: &enginev1.CourseOutline{
				Title: topic,
				Units: []*enginev1.Unit{{Title: "Unit 1", Lessons: []*enginev1.LessonRef{{Title: "Lesson 1", Goal: "Goal"}}}},
			},
			ExampleSlides: []*enginev1.Slide{{SlideId: "s1", Html: `<div id="root">Fallback content</div>`, Narration: "Fallback narration", DurationMs: 1000}},
		}, nil

	case strings.HasPrefix(topic, "5xx-outage"):
		// Both providers fail with 5xx outage
		p.attempts = []*enginev1.Usage{
			{
				Provider:         req.Limits.Provider,
				Model:            req.Limits.Model,
				Attempt:          1,
				FallbackUsed:     false,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: true,
				ErrorCode:        "provider_outage",
			},
			{
				Provider:         req.Limits.FallbackProvider,
				Model:            req.Limits.FallbackModel,
				Attempt:          2,
				FallbackUsed:     true,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: true,
				ErrorCode:        "provider_outage",
			},
		}
		return nil, status.Error(codes.Unavailable, "provider_outage")

	case strings.HasPrefix(topic, "timeout"):
		select {
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-time.After(2 * time.Second):
			return nil, status.Error(codes.DeadlineExceeded, "timeout")
		}

	case strings.HasPrefix(topic, "invalid-schema"):
		p.attempts = []*enginev1.Usage{
			{
				Provider:         req.Limits.Provider,
				Model:            req.Limits.Model,
				InputTokens:      50,
				OutputTokens:     20,
				Attempt:          1,
				FallbackUsed:     false,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: false,
				ErrorCode:        "invalid_schema",
			},
			{
				Provider:         req.Limits.FallbackProvider,
				Model:            req.Limits.FallbackModel,
				InputTokens:      50,
				OutputTokens:     20,
				Attempt:          2,
				FallbackUsed:     true,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: false,
				ErrorCode:        "invalid_schema",
			},
		}
		return &enginev1.PreviewCourseResponse{
			Attempts:    p.attempts,
			Unavailable: true,
		}, nil

	default:
		return &enginev1.PreviewCourseResponse{
			Attempts: []*enginev1.Usage{{Provider: req.Limits.Provider, Model: req.Limits.Model, InputTokens: 100, OutputTokens: 30, Attempt: 1, Component: "content", Delivered: true}},
			SampleOutline: &enginev1.CourseOutline{
				Title: topic,
				Units: []*enginev1.Unit{{Title: "Unit 1", Lessons: []*enginev1.LessonRef{{Title: "Lesson 1", Goal: "Goal"}}}},
			},
			ExampleSlides: []*enginev1.Slide{{SlideId: "s1", Html: `<div id="root">OK</div>`, Narration: "OK narration", DurationMs: 1000}},
		}, nil
	}
}

func TestModelRoutingFailuresE2E(t *testing.T) {
	grpcListener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	double := &providerDouble{}
	backend := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(backend, double)
	go func() { _ = backend.Serve(grpcListener) }()
	t.Cleanup(backend.Stop)

	client, err := engine.New("http://"+grpcListener.Addr().String(), 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	auth, access, database := signedInDatabase(t)
	claims, err := auth.ParseAccess(access)
	if err != nil {
		t.Fatal(err)
	}
	owner := claims.Subject

	var mu sync.Mutex
	var capturedEvents []map[string]any
	posthog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Batch []map[string]any `json:"batch"`
		}
		if decErr := json.NewDecoder(r.Body).Decode(&batch); decErr == nil {
			mu.Lock()
			capturedEvents = append(capturedEvents, batch.Batch...)
			mu.Unlock()
		}
		w.WriteHeader(200)
	}))
	defer posthog.Close()

	events, err := analytics.NewWithOptions("test-posthog-key", posthog.URL, analytics.Options{Consent: func(context.Context, string) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()

	catalogue, err := pricing.Load(t.Context(), database.Queries)
	if err != nil {
		t.Fatal(err)
	}
	creditService := credits.NewServiceWithCatalogue(database.Pool, nil, catalogue, events)
	quotes := pricing.NewQuotes(database.Pool)

	// Fund learner wallet with initial credits
	if _, err = database.Pool.Exec(t.Context(), "INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,1000,'purchase','e10-03-init',$2)", owner, catalogue.Config().Version); err != nil {
		t.Fatal(err)
	}
	if _, err = creditService.EnsureDailyGrant(t.Context(), owner, time.Now()); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := httpapi.New(httpapi.Dependencies{
		Accounts:    auth,
		CourseBrief: courses.New(courses.Options{Pool: database.Pool}),
		Engine:      client,
		Credits:     creditService,
		Quotes:      quotes,
		Postgres:    func(context.Context) error { return nil },
		Redis:       func(context.Context) error { return errors.New("unavailable") },
	}, logger)
	server := httpapi.NewServer("127.0.0.1:0", handler)
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	serverCtx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- httpapi.Serve(serverCtx, server, listener) }()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Log(err)
		}
	})

	httpClient := &http.Client{Timeout: 3 * time.Second}

	callPreview := func(topic string) (int, string) {
		brief := courses.Brief{Topic: topic}
		raw, _ := json.Marshal(brief)
		var revision string
		if e := database.Pool.QueryRow(t.Context(), `INSERT INTO course_brief_revisions(learner_id,brief,content_hash) VALUES($1,$2,$3) RETURNING id`, owner, raw, brief.Hash()).Scan(&revision); e != nil {
			t.Fatal(e)
		}
		quote, e := quotes.Create(t.Context(), owner, pricing.QuoteInput{Action: "preview", BriefRevision: revision})
		if e != nil {
			t.Fatal(e)
		}
		if e = quotes.Accept(t.Context(), owner, quote.ID); e != nil {
			t.Fatal(e)
		}
		body, _ := json.Marshal(map[string]string{"brief_revision": revision})
		req, e := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+listener.Addr().String()+"/v1/courses/preview", strings.NewReader(string(body)))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("X-Quote-ID", quote.ID)
		resp, e := httpClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		respBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode, string(respBytes)
	}

	getWalletBalance := func() int64 {
		w, e := creditService.GetWallet(t.Context(), owner)
		if e != nil {
			t.Fatal(e)
		}
		return w.Available
	}

	t.Run("429 rate limit recovers via fallback within 2-attempt budget", func(t *testing.T) {
		before := getWalletBalance()
		statusCode, body := callPreview("429-fallback-topic")
		if statusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on fallback recovery, got %d: %s", statusCode, body)
		}
		if !strings.Contains(body, "Fallback content") {
			t.Fatalf("expected fallback slide content, got: %s", body)
		}
		after := getWalletBalance()
		if after >= before {
			t.Fatalf("expected credits settled on successful delivery, before=%d, after=%d", before, after)
		}
	})

	t.Run("5xx provider outage on primary and fallback releases all reserved credits", func(t *testing.T) {
		before := getWalletBalance()
		statusCode, body := callPreview("5xx-outage-topic")
		if statusCode != http.StatusBadGateway && statusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 502/503 on provider outage, got %d: %s", statusCode, body)
		}
		after := getWalletBalance()
		if after != before {
			t.Fatalf("expected 100%% reservation release on terminal failure, before=%d, after=%d", before, after)
		}
	})

	t.Run("timeout terminates within deadline and releases reservation in full", func(t *testing.T) {
		before := getWalletBalance()
		statusCode, body := callPreview("timeout-topic")
		if statusCode != http.StatusGatewayTimeout && statusCode != http.StatusBadGateway {
			t.Fatalf("expected 504/502 on timeout, got %d: %s", statusCode, body)
		}
		after := getWalletBalance()
		if after != before {
			t.Fatalf("expected full reservation release on timeout, before=%d, after=%d", before, after)
		}
	})

	t.Run("invalid schema returns typed error and releases reservation", func(t *testing.T) {
		before := getWalletBalance()
		statusCode, body := callPreview("invalid-schema-topic")
		if statusCode != http.StatusBadGateway && statusCode != http.StatusUnprocessableEntity && statusCode != http.StatusInternalServerError {
			t.Fatalf("expected error on invalid schema, got %d: %s", statusCode, body)
		}
		after := getWalletBalance()
		if after != before {
			t.Fatalf("expected full reservation release on invalid schema, before=%d, after=%d", before, after)
		}
	})

	// Verify eval package exists and provides typed error bounds helper
	t.Run("eval package routing bounds verify", func(t *testing.T) {
		res := eval.VerifyModelRoutingBounds(eval.RoutingConfig{
			MaxAttempts: 2,
		})
		if !res.Valid {
			t.Fatalf("expected valid routing bounds: %v", res.Reason)
		}
	})
}
