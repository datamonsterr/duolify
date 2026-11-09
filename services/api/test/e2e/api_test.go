// Package e2e exercises the real HTTP server and gRPC transport without external services.
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeEngine struct {
	enginev1.UnimplementedEngineServiceServer
}

func (*fakeEngine) Health(context.Context, *enginev1.HealthRequest) (*enginev1.HealthResponse, error) {
	return &enginev1.HealthResponse{Version: "test"}, nil
}
func (*fakeEngine) PreviewCourse(ctx context.Context, req *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	if req.GetTopic() == "fail" {
		return nil, status.Error(codes.Unavailable, "private diagnostic")
	}
	if req.GetTopic() == "slow" {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	return &enginev1.PreviewCourseResponse{SampleOutline: &enginev1.CourseOutline{Title: req.GetTopic()}, ExampleSlides: []*enginev1.Slide{{SlideId: "demo", Html: "<p>Learn</p>"}}}, nil
}

func TestHTTPAPI(t *testing.T) {
	grpcListener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	backend := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(backend, &fakeEngine{})
	go func() {
		if serveErr := backend.Serve(grpcListener); serveErr != nil {
			t.Errorf("grpc serve: %v", serveErr)
		}
	}()
	t.Cleanup(backend.Stop)
	client, err := engine.New("http://"+grpcListener.Addr().String(), 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	auth, access := signedIn(t)
	handler := httpapi.New(httpapi.Dependencies{Accounts: auth, Engine: client, Postgres: func(context.Context) error { return nil }, Redis: func(context.Context) error { return errors.New("unavailable") }}, logger)
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
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	httpClient := &http.Client{Timeout: 2 * time.Second, Transport: transport}
	for _, tt := range []struct {
		name, method, path, body string
		status                   int
		contains                 string
	}{
		{"health", http.MethodGet, "/healthz", "", 200, `"status":"ok"`},
		{"courses", http.MethodGet, "/v1/courses", "", 200, `"title"`},
		{"anonymous courses", http.MethodGet, "/v1/courses", "", 401, `"error"`},
		{"learner", http.MethodGet, "/v1/me", "", 200, `"display_name":"Learner"`},
		{"preview", http.MethodPost, "/v1/courses/preview", `{"topic":"Physics"}`, 200, `"Physics"`},
		{"readiness", http.MethodGet, "/readyz", "", 503, `"redis":"error"`},
		{"invalid", http.MethodPost, "/v1/courses/preview", `{`, 400, `"error"`},
		{"missing topic", http.MethodPost, "/v1/courses/preview", `{"topic":" "}`, 400, `"error"`},
		{"trailing json", http.MethodPost, "/v1/courses/preview", `{"topic":"x"}{}`, 400, `"error"`},
		{"engine error", http.MethodPost, "/v1/courses/preview", `{"topic":"fail"}`, 502, `"error"`},
		{"engine timeout", http.MethodPost, "/v1/courses/preview", `{"topic":"slow"}`, 504, `"error"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), tt.method, "http://"+listener.Addr().String()+tt.path, strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if tt.name != "anonymous courses" {
				req.Header.Set("Authorization", "Bearer "+access)
			}
			resp, err := httpClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Error(err)
				}
			}()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.status || !strings.Contains(string(body), tt.contains) || !json.Valid(body) {
				t.Fatalf("status=%d body=%s", resp.StatusCode, body)
			}
			if resp.Header.Get("X-Request-ID") == "" {
				t.Error("missing request ID")
			}
			if strings.Contains(string(body), "private diagnostic") {
				t.Error("engine diagnostic exposed")
			}
		})
	}
}
