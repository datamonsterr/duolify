//go:build integration

package eval_test

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/eval"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func swallowedErrorsService(t *testing.T) (*eval.Service, *store.Store) {
	t.Helper()
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("eval_swallowed"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	t.Cleanup(database.Close)
	return eval.NewService(database.Pool, nil, nil, nil), database
}

// A transient history-insert failure must not suppress the alert key for the dedupe window:
// the next run records the row.
func TestFailedAlertHistoryInsertDoesNotPoisonDedupeCacheIntegration(t *testing.T) {
	ctx := t.Context()
	svc, database := swallowedErrorsService(t)
	for _, q := range []string{
		`CREATE FUNCTION fail_alert_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected'; END $$`,
		`CREATE TRIGGER fail_alert_insert BEFORE INSERT ON eval_alert_events FOR EACH ROW EXECUTE FUNCTION fail_alert_insert()`,
	} {
		if _, err := database.Pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	svc.RaiseOperationalAlert(ctx, "pricing_load")
	if _, err := database.Pool.Exec(ctx, `DROP TRIGGER fail_alert_insert ON eval_alert_events`); err != nil {
		t.Fatal(err)
	}
	svc.RaiseOperationalAlert(ctx, "pricing_load")
	var rows int
	if err := database.Pool.QueryRow(ctx, `SELECT count(*) FROM eval_alert_events WHERE scope='pricing_load'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("history rows after retry = %d, want 1 (failed insert cached the key)", rows)
	}
}

// A failed read must surface, never read as zero spend (which would silently skip budget alerts).
func TestAlertAndDashboardReadsSurfaceDatabaseErrorsIntegration(t *testing.T) {
	ctx := t.Context()
	svc, database := swallowedErrorsService(t)
	// No counter row yet is a normal zero, not an error.
	if _, err := svc.CheckAndTriggerAlerts(ctx); err != nil {
		t.Fatalf("empty counters must not fail: %v", err)
	}
	if _, err := svc.GetDashboards(ctx); err != nil {
		t.Fatalf("empty dashboards must not fail: %v", err)
	}
	if _, err := database.Pool.Exec(ctx, `ALTER TABLE credit_cost_counters RENAME TO credit_cost_counters_gone`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CheckAndTriggerAlerts(ctx); err == nil {
		t.Fatal("CheckAndTriggerAlerts hid a failed spend read")
	}
	if _, err := svc.GetDashboards(ctx); err == nil {
		t.Fatal("GetDashboards hid a failed spend read")
	}
}
