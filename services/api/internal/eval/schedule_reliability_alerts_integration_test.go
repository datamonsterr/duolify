//go:build integration

package eval_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/eval"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Two worker processes share one database: the periodic job must run, emit one
// alert per window and not double-fire across replicas.
func TestScheduleReliabilityAlertsRunsPeriodicJobAndDedupesAcrossWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("eval_sched_alerts"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	database, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var mu sync.Mutex
	var captured []map[string]any
	posthog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Batch []map[string]any `json:"batch"`
		}
		if json.NewDecoder(r.Body).Decode(&batch) == nil {
			mu.Lock()
			captured = append(captured, batch.Batch...)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer posthog.Close()
	events, err := analytics.NewWithOptions("test-key", posthog.URL, analytics.Options{Consent: func(context.Context, string) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}

	// Seed one failed call out of two: error rate 50% and fallback rate 50%.
	seed := eval.NewService(database.Pool, nil, telemetry.New(nil), events)
	for _, m := range []eval.CallMetrics{
		{Task: "course_preview", Success: true, FallbackUsed: true, InputTokens: 10, CacheReadTokens: 100},
		{Task: "course_preview", Success: false, ErrorCode: "provider_outage", InputTokens: 10, CacheReadTokens: 100},
	} {
		if err = seed.RecordCallMetrics(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err = worker.Migrate(ctx, database.Pool, logger); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		svc := eval.NewService(database.Pool, nil, telemetry.New(nil), events)
		queue, qerr := worker.NewWithAlerts(database.Pool, logger, telemetry.New(logger), nil, svc)
		if qerr != nil {
			t.Fatal(qerr)
		}
		if err = queue.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer func() {
			stop, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			_ = queue.Stop(stop)
		}()
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		var done int
		if err = database.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind='reliability_alerts' AND state='completed'").Scan(&done); err != nil {
			t.Fatal(err)
		}
		if done > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("periodic reliability_alerts job never completed")
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, scope := range []string{"error_rate", "fallback_rate"} {
		var n int
		if err = database.Pool.QueryRow(ctx, "SELECT count(*) FROM eval_alert_events WHERE scope=$1", scope).Scan(&n); err != nil || n != 1 {
			t.Fatalf("scope %s alert rows=%d err=%v", scope, n, err)
		}
	}

	// A further run is deduplicated and emits nothing new.
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	emitted := len(captured)
	mu.Unlock()
	results, err := seed.CheckAndTriggerAlerts(ctx)
	if err != nil || len(results) == 0 {
		t.Fatalf("results=%v err=%v", results, err)
	}
	for _, r := range results {
		if !r.Deduplicated {
			t.Fatalf("expected %s to be deduplicated", r.Scope)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	budgetEvents := 0
	for _, e := range captured {
		if e["event"] == "ai_cost_budget_alert" {
			budgetEvents++
		}
	}
	if budgetEvents != 2 || emitted != len(captured) {
		t.Fatalf("ai_cost_budget_alert events=%d (want 2: error_rate, fallback_rate)", budgetEvents)
	}
}

func TestRaiseOperationalAlertDedupesAndExpiredReportsArePurged(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("eval_op_alert"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	database, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	svc := eval.NewService(database.Pool, nil, telemetry.New(nil), nil)

	for range 3 {
		svc.RaiseOperationalAlert(ctx, "pricing_config_load_failed")
	}
	var n int
	if err = database.Pool.QueryRow(ctx, "SELECT count(*) FROM eval_alert_events WHERE scope='pricing_config_load_failed'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("alert rows=%d err=%v", n, err)
	}

	if _, err = database.Pool.Exec(ctx, `INSERT INTO eval_reports(task,dataset_revision,model,score,threshold,passed,report_json,expires_at)
		VALUES('old','r1','m',1,1,true,'{}', now() - interval '1 day'), ('new','r1','m',1,1,true,'{}', now() + interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	if err = svc.RunScheduledAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	var tasks int
	if err = database.Pool.QueryRow(ctx, "SELECT count(*) FROM eval_reports WHERE task='old'").Scan(&tasks); err != nil || tasks != 0 {
		t.Fatalf("expired reports kept=%d err=%v", tasks, err)
	}
	if err = database.Pool.QueryRow(ctx, "SELECT count(*) FROM eval_reports WHERE task='new'").Scan(&tasks); err != nil || tasks != 1 {
		t.Fatalf("live reports=%d err=%v", tasks, err)
	}
}
