package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRestoreDrillRestoresSyntheticDumpAndFailsInjectedCorruption(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.RunJob(t, "2026-11-12")

	options := fixture.DrillOptions(t, time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC))
	status := 0
	options.Inspect = func(ctx context.Context, restored *pgxpool.Pool) error {
		server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Postgres: restored.Ping}, fixture.Logger))
		defer server.Close()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/healthz", nil)
		if err != nil {
			return err
		}
		response, err := server.Client().Do(request)
		if err != nil {
			return err
		}
		defer func() { _ = response.Body.Close() }()
		status = response.StatusCode
		return nil
	}
	report, err := backup.RunDrill(t.Context(), options)
	if err != nil || !report.OK {
		t.Fatalf("synthetic restore drill failed: %v %+v", err, report)
	}
	if status != http.StatusOK || report.RPO != 20*time.Hour || report.DumpKey != fixture.ArchiveKey("2026-11-12") {
		t.Fatalf("status=%d report=%+v", status, report)
	}

	// Injected corruption: the newest dump lacks an applied migration. ci-ok must
	// fail, which in CI is this error becoming a non-zero exit of the mise task.
	if _, err := fixture.DB.Pool.Exec(t.Context(), "DELETE FROM goose_db_version WHERE version_id=(SELECT max(version_id) FROM goose_db_version)"); err != nil {
		t.Fatal(err)
	}
	fixture.RunJob(t, "2026-11-13")
	bad, err := backup.RunDrill(t.Context(), fixture.DrillOptions(t, time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)))
	if err == nil || bad.OK {
		t.Fatalf("injected corruption passed the drill: %+v", bad)
	}
	failed := false
	for _, check := range bad.Checks {
		failed = failed || (check.Name == "migration registry" && !check.OK)
	}
	if !failed {
		t.Fatalf("migration registry check did not flag corruption: %+v", bad.Checks)
	}
	fixture.AssertDrillEvents(t, []bool{true, false})
}
