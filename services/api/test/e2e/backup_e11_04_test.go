package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type traceLog struct {
	sync.Mutex
	bytes.Buffer
}

func (l *traceLog) Write(b []byte) (int, error) { l.Lock(); defer l.Unlock(); return l.Buffer.Write(b) }
func (l *traceLog) snapshot() string            { l.Lock(); defer l.Unlock(); return l.String() }

type traceEngine struct {
	enginev1.UnimplementedEngineServiceServer
	ids chan metadata.MD
}

func (e *traceEngine) Health(ctx context.Context, _ *enginev1.HealthRequest) (*enginev1.HealthResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	e.ids <- md.Copy()
	return &enginev1.HealthResponse{}, nil
}
func e11Addr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestFailedHTTPActionCorrelatesGRPCAndRiverAlert(t *testing.T) {
	logs := &traceLog{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	monitor := telemetry.New(logger)
	fixture := backuptest.NewObserved(t, monitor, logger)
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", e11Addr("E11_GRPC_E2E_ADDR", "127.0.0.1:4115"))
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer()
	double := &traceEngine{ids: make(chan metadata.MD, 1)}
	enginev1.RegisterEngineServiceServer(rpc, double)
	go func() { _ = rpc.Serve(listener) }()
	t.Cleanup(rpc.Stop)
	client, err := engine.New("http://"+listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	jobs := make(chan int64, 1)
	server := httptest.NewUnstartedServer(httpapi.Middleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := client.Health(r.Context()); err != nil {
			t.Error(err)
		}
		id, err := fixture.Enqueue(r.Context(), "invalid-date")
		if err != nil {
			t.Error(err)
		}
		jobs <- id
		w.WriteHeader(http.StatusBadGateway)
	}), monitor))
	_ = server.Listener.Close()
	server.Listener, err = new(net.ListenConfig).Listen(t.Context(), "tcp", e11Addr("E11_API_E2E_ADDR", "127.0.0.1:4114"))
	if err != nil {
		t.Fatal(err)
	}
	server.Start()
	defer server.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/action?token=private-token", strings.NewReader("raw-prompt private-text"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer private-token")
	req.Header.Set("X-Request-ID", "private-text")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 502 {
		t.Fatal(response.StatusCode)
	}
	id := response.Header.Get("X-Request-ID")
	md := <-double.ids
	if id == "" || id == "private-text" || md.Get("x-request-id")[0] != id {
		t.Fatalf("correlation lost: %s %v", id, md)
	}
	fixture.WaitDiscarded(t, <-jobs)
	fixture.AssertEvents(t, "backup_failed", "backup_failed", "backup_failed")
	metrics := httptest.NewRecorder()
	monitor.ServeHTTP(metrics, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	if !strings.Contains(metrics.Body.String(), `duolify_alerts_total{kind="backup"} 1`) {
		t.Fatal(metrics.Body.String())
	}
	text := logs.snapshot()
	for _, private := range []string{"private-token", "raw-prompt", "private-text", "invalid-date"} {
		if strings.Contains(text, private) {
			t.Fatalf("private telemetry: %s", text)
		}
	}
	var trace string
	operations := map[string]int{}
	decoder := json.NewDecoder(strings.NewReader(text))
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record["msg"] != "span" || record["request_id"] != id {
			continue
		}
		if trace == "" {
			trace = record["trace_id"].(string)
		}
		if record["trace_id"] != trace {
			t.Fatal("trace forked")
		}
		operations[record["operation"].(string)]++
	}
	if operations["http"] != 1 || operations["grpc"] != 1 || operations["job"] != 3 {
		t.Fatalf("missing correlated spans: %v\n%s", operations, text)
	}
}
