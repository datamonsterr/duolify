//go:build integration

package backup_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRiverRetryPreservesTraceAndBackupRestoresMetadata(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	monitor := telemetry.New(logger)
	fixture := backuptest.NewObserved(t, monitor, logger)
	fixture.Seed(t)
	ctx := telemetry.Root(t.Context())
	correlation := telemetry.CorrelationFrom(ctx)
	fixture.FailNextUpload()
	id, err := fixture.Enqueue(ctx, "2026-11-07")
	if err != nil {
		t.Fatal(err)
	}
	fixture.WaitCompleted(t, id)
	fixture.RunJob(t, "2026-11-08")
	restored := fixture.Restore(t, fixture.Ciphertext(t, fixture.ArchiveKey("2026-11-08")))
	fixture.AssertCorrelationRestored(t, restored, id, correlation)
	fixture.AssertEvents(t, "backup_failed", "backup_completed", "backup_completed")
	metrics := httptest.NewRecorder()
	monitor.ServeHTTP(metrics, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	if strings.Contains(metrics.Body.String(), `duolify_alerts_total{kind="backup"} 1`) {
		t.Fatal("retry raised terminal alert")
	}
	if !strings.Contains(metrics.Body.String(), `duolify_operations_total{operation="job",ok="false"} 1`) {
		t.Fatal(metrics.Body.String())
	}
}

type unavailableCollector struct{}

func (unavailableCollector) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestPostHogOutageNeverBlocksOperationalAlerts(t *testing.T) {
	double := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer double.Close()
	events, err := analytics.New("test-project", double.URL, func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()
	monitor := telemetry.New(slog.New(slog.NewJSONHandler(unavailableCollector{}, nil)))
	for range 10000 {
		_ = events.Capture("00000000-0000-4000-8000-000000000011", "backup_failed", map[string]any{"source": "api", "platform": "web", "app_version": "test", "ok": false, "size_bytes": 0, "duration_ms": 1})
	}
	fixture := backuptest.New(t)
	pool, err := pgxpool.NewWithConfig(t.Context(), fixture.DB.Pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	queue, err := worker.NewObserved(pool, fixture.Logger, monitor)
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	watchCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); worker.WatchQueue(watchCtx, monitor, pool.Ping, time.Millisecond) }()
	for range 2 {
		if _, err := queue.Insert(t.Context(), worker.DummyArgs{}, nil); err == nil {
			t.Fatal("broken queue accepted job")
		}
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		check := httptest.NewRecorder()
		monitor.ServeHTTP(check, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
		if strings.Contains(check.Body.String(), `duolify_alerts_total{kind="queue"} 1`) {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("queue watchdog did not observe pool failure")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-watchDone
	recorder := httptest.NewRecorder()
	monitor.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	if !strings.Contains(recorder.Body.String(), `duolify_alerts_total{kind="queue"} 1`) {
		t.Fatal(recorder.Body.String())
	}
}
