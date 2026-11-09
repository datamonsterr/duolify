package backup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestOperationalIncidentsDeduplicateAndRecoverWithoutAnalytics(t *testing.T) {
	var logs bytes.Buffer
	monitor := telemetry.New(slog.New(slog.NewJSONHandler(&logs, nil)))
	ctx := telemetry.Root(t.Context())
	for _, kind := range []string{"backup", "queue", "dependency"} {
		for range 3 {
			monitor.Incident(ctx, kind, "one", false)
		}
	}
	monitor.Incident(ctx, "dependency", "one", true)
	monitor.Incident(ctx, "dependency", "one", false)
	recorder := httptest.NewRecorder()
	monitor.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	for _, want := range []string{`duolify_alerts_total{kind="backup"} 1`, `duolify_alerts_total{kind="queue"} 1`, `duolify_alerts_total{kind="dependency"} 2`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatal(recorder.Body.String())
		}
	}
	if strings.Count(logs.String(), `"msg":"operational alert"`) != 4 {
		t.Fatal(logs.String())
	}
}

func TestWorkerMetadataRetrySpansAndPrivatePanic(t *testing.T) {
	var logs bytes.Buffer
	monitor := telemetry.New(slog.New(slog.NewJSONHandler(&logs, nil)))
	middleware := worker.NewTelemetry(monitor)
	ctx := telemetry.Root(t.Context())
	correlation := telemetry.CorrelationFrom(ctx)
	params := []*rivertype.JobInsertParams{{Metadata: []byte(`{"existing":true}`)}}
	_, err := middleware.InsertMany(ctx, params, func(context.Context) ([]*rivertype.JobInsertResult, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(params[0].Metadata, &persisted); err != nil || persisted["existing"] != true {
		t.Fatal(string(params[0].Metadata), err)
	}
	job := &rivertype.JobRow{ID: 7, Kind: "database_backup", Metadata: params[0].Metadata, MaxAttempts: 3}
	for attempt := 1; attempt <= 3; attempt++ {
		job.Attempt = attempt
		err := middleware.Work(t.Context(), job, func(work context.Context) error {
			actual := telemetry.CorrelationFrom(work)
			if actual.RequestID != correlation.RequestID || actual.TraceID != correlation.TraceID || actual.ParentSpanID != correlation.SpanID {
				t.Errorf("lost parent: %+v", actual)
			}
			if attempt == 2 {
				panic("raw-prompt bearer-token private-text")
			}
			return errors.New("raw-prompt bearer-token private-text")
		})
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal(err)
		}
	}
	_ = middleware.Work(t.Context(), job, func(context.Context) error { return errors.New("private-text") })
	for _, private := range []string{"raw-prompt", "bearer-token", "private-text", "existing"} {
		if strings.Contains(logs.String(), private) {
			t.Fatal(logs.String())
		}
	}
	if strings.Count(logs.String(), `"msg":"operational alert"`) != 1 {
		t.Fatal(logs.String())
	}
	malformed := telemetry.Restore(t.Context(), telemetry.Correlation{RequestID: "private-text", TraceID: "invalid", SpanID: "invalid"})
	if telemetry.CorrelationFrom(malformed).RequestID == "private-text" {
		t.Fatal("untrusted correlation reflected")
	}
}

func TestReadinessNamesEveryDependencyAndDeduplicatesOutages(t *testing.T) {
	monitor := telemetry.New(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for _, failed := range []string{"postgres", "redis", "engine", "object_storage"} {
		t.Run(failed, func(t *testing.T) {
			probe := func(name string) func(context.Context) error {
				return func(context.Context) error {
					if name == failed {
						return errors.New("private-token private-text")
					}
					return nil
				}
			}
			handler := httpapi.New(httpapi.Dependencies{Postgres: probe("postgres"), Redis: probe("redis"), Engine: healthEngine{check: probe("engine")}, ObjectStorage: probe("object_storage"), Monitor: monitor}, slog.Default())
			for range 2 {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", "/readyz", nil))
				var checks map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &checks); err != nil {
					t.Fatal(err)
				}
				if w.Code != 503 || len(checks) != 4 || checks[failed] != "error" || strings.Contains(w.Body.String(), "private") {
					t.Fatal(w.Code, w.Body.String())
				}
			}
		})
	}
	recorder := httptest.NewRecorder()
	monitor.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	if !strings.Contains(recorder.Body.String(), `duolify_alerts_total{kind="dependency"} 4`) {
		t.Fatal(recorder.Body.String())
	}
}

type healthEngine struct {
	httpapi.Engine
	check func(context.Context) error
}

func TestWorkerControlsAndShutdownDoNotRaiseFailureAlerts(t *testing.T) {
	monitor := telemetry.New(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	middleware := worker.NewTelemetry(monitor)
	job := &rivertype.JobRow{ID: 9, Kind: "database_backup", Attempt: 3, MaxAttempts: 3}
	var snooze *river.JobSnoozeError
	err := middleware.Work(t.Context(), job, func(context.Context) error { return river.JobSnooze(time.Minute) })
	if !errors.As(err, &snooze) || snooze.Duration != time.Minute {
		t.Fatal("snooze semantics lost", err)
	}
	var canceled *river.JobCancelError
	err = middleware.Work(t.Context(), job, func(context.Context) error { return river.JobCancel(errors.New("private-text")) })
	if !errors.As(err, &canceled) || strings.Contains(err.Error(), "private-text") {
		t.Fatal("cancel semantics or redaction lost", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = middleware.Work(ctx, job, func(context.Context) error { return context.Canceled })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown cancellation lost", err)
	}
	recorder := httptest.NewRecorder()
	monitor.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	if !strings.Contains(recorder.Body.String(), `duolify_alerts_total{kind="backup"} 0`) {
		t.Fatal(recorder.Body.String())
	}
}

func TestIdleQueueWatchdogDeduplicatesDependencyFailure(t *testing.T) {
	monitor := telemetry.New(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probes := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.WatchQueue(ctx, monitor, func(ctx context.Context) error {
			select {
			case probes <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case <-release:
				return errors.New("private postgres diagnostic")
			case <-ctx.Done():
				return ctx.Err()
			}
		}, time.Millisecond)
	}()
	for range 3 {
		select {
		case <-probes:
		case <-time.After(time.Second):
			t.Fatal("watchdog failed to probe")
		}
		release <- struct{}{}
	}
	// The next probe starts only after the preceding incident has been recorded.
	select {
	case <-probes:
	case <-time.After(time.Second):
		t.Fatal("watchdog stalled")
	}
	recorder := httptest.NewRecorder()
	monitor.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	if !strings.Contains(recorder.Body.String(), `duolify_alerts_total{kind="queue"} 1`) {
		t.Fatal(recorder.Body.String())
	}
	cancel()
	<-done
}

func (h healthEngine) Health(ctx context.Context) error { return h.check(ctx) }

func TestPostHogAdapterRejectsPrivatePropertiesAndIsNonblocking(t *testing.T) {
	var mu sync.Mutex
	var batches []map[string]any
	double := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Error(err)
		}
		mu.Lock()
		batches = append(batches, batch.Batch...)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"status":"Ok"}`)
	}))
	defer double.Close()
	events, err := analytics.New("test-key", double.URL)
	if err != nil {
		t.Fatal(err)
	}
	properties := map[string]any{"source": "api", "platform": "web", "app_version": "test", "size_bytes": 0, "duration_ms": 1, "ok": false, "prompt": "private-text", "token": "private-token", "$ai_input": "raw-prompt"}
	if err := events.Capture("00000000-0000-4000-8000-000000000011", "backup_failed", properties); err != nil {
		t.Fatal(err)
	}
	_ = events.Capture("private-text", "backup_failed", properties)
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	encoded, _ := json.Marshal(batches)
	mu.Unlock()
	if len(batches) != 1 || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "raw-prompt") {
		t.Fatal(string(encoded))
	}
	optedOut, err := analytics.NewWithOptions("test-key", double.URL, analytics.Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = optedOut.Capture("00000000-0000-4000-8000-000000000011", "backup_failed", properties)
	_ = optedOut.Close()
	mu.Lock()
	count := len(batches)
	mu.Unlock()
	if count != 1 {
		t.Fatal("opted-out operations event delivered")
	}
	outage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer outage.Close()
	adapter, err := analytics.New("test-key", outage.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = adapter.Close() }()
	start := time.Now()
	for range 20000 {
		_ = adapter.Capture("00000000-0000-4000-8000-000000000011", "backup_failed", properties)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("analytics blocked on remote outage")
	}
}
