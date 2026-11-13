//go:build integration

package backup_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/jackc/pgx/v5/pgxpool"
)

func drillNow(day string) time.Time {
	d, _ := time.Parse(time.DateOnly, day)
	return d.Add(6 * time.Hour)
}

func TestDrillRestoresThroughRestorerIdentityAndPassesAllCheckKinds(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.RunJob(t, "2026-11-12")
	options := fixture.DrillOptions(t, drillNow("2026-11-12"))
	report, err := backup.RunDrill(t.Context(), options)
	if err != nil || !report.OK {
		t.Fatalf("drill failed: %v %+v", err, report)
	}
	kinds := map[backup.CheckKind]int{}
	for _, c := range report.Checks {
		if !c.OK {
			t.Fatalf("check %q failed", c.Name)
		}
		kinds[c.Kind]++
	}
	for _, kind := range []backup.CheckKind{backup.KindSchema, backup.KindData, backup.KindObjects, backup.KindPrivacy} {
		if kinds[kind] == 0 {
			t.Fatalf("no %s checks ran: %+v", kind, report.Checks)
		}
	}
	if report.RPO != 6*time.Hour || report.RTO <= 0 || report.RTO > 4*time.Hour || report.FinancialReplay != "not-configured" {
		t.Fatalf("measurements: %+v", report)
	}
	if report.RedisRebuilt == 0 {
		t.Fatal("Redis was not rebuilt from restored Postgres")
	}
}

func TestDrillAppliesDeletionTombstonesFromObjectLedger(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.RunJob(t, "2026-11-12")
	fixture.DeleteLearnerInLedgerOnly(t, "a1111111-1111-4111-8111-111111111111")
	options := fixture.DrillOptions(t, drillNow("2026-11-12"))
	var survived bool
	options.Inspect = func(ctx context.Context, restored *pgxpool.Pool) error {
		return restored.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM learners WHERE id='a1111111-1111-4111-8111-111111111111')").Scan(&survived)
	}
	if report, err := backup.RunDrill(t.Context(), options); err != nil || !report.OK {
		t.Fatalf("drill: %v %+v", err, report)
	}
	if survived {
		t.Fatal("deleted learner resurrected in drill output")
	}
}

func TestAnonymizerRemovesSeededPIIAndRegistryClassifiesEveryTextColumn(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.RunJob(t, "2026-11-12")
	options := fixture.DrillOptions(t, drillNow("2026-11-12"))
	var before, after int
	options.BeforeAnonymize = func(ctx context.Context, p *pgxpool.Pool) error {
		return p.QueryRow(ctx, "SELECT count(*) FROM learners WHERE display_name LIKE 'Synthetic Learner%'").Scan(&before)
	}
	options.Inspect = func(ctx context.Context, p *pgxpool.Pool) error {
		return p.QueryRow(ctx, "SELECT count(*) FROM learners WHERE display_name LIKE 'Synthetic Learner%'").Scan(&after)
	}
	if report, err := backup.RunDrill(t.Context(), options); err != nil || !report.OK {
		t.Fatalf("drill: %v %+v", err, report)
	}
	if before == 0 || after != 0 {
		t.Fatalf("seeded display names before=%d after=%d", before, after)
	}
}

func TestPrivacyChecksFailClosedOnUnclassifiedColumnAndLeakedEmail(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.RunJob(t, "2026-11-12")
	options := fixture.DrillOptions(t, drillNow("2026-11-12"))
	options.BeforeAnonymize = func(ctx context.Context, p *pgxpool.Pool) error {
		_, err := p.Exec(ctx, "CREATE TABLE feature_notes(id int PRIMARY KEY, email_address text); INSERT INTO feature_notes VALUES(1,'person@example.com')")
		return err
	}
	report, err := backup.RunDrill(t.Context(), options)
	if err == nil || report.OK {
		t.Fatal("a new unregistered text column carrying an email must fail the drill")
	}
	var names []string
	for _, c := range report.Checks {
		if !c.OK {
			names = append(names, c.Name)
		}
	}
	if !strings.Contains(strings.Join(names, ","), "pii column classification") {
		t.Fatalf("failed checks: %v", names)
	}
}

func TestObjectReferenceCheckFailsWhenReferencedObjectIsLost(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.RunJob(t, "2026-11-12")
	fixture.LoseApplicationObject(t)
	report, err := backup.RunDrill(t.Context(), fixture.DrillOptions(t, drillNow("2026-11-12")))
	if err == nil || report.OK {
		t.Fatal("missing application object must fail the drill")
	}
	for _, c := range report.Checks {
		if c.Kind == backup.KindObjects && c.OK {
			continue
		}
		if c.Kind == backup.KindObjects {
			return
		}
	}
	t.Fatalf("object check did not report: %+v", report.Checks)
}

func TestMigrationRegistryCoversEveryEmbeddedMigration(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.RunJob(t, "2026-11-12")
	report, err := backup.RunDrill(t.Context(), fixture.DrillOptions(t, drillNow("2026-11-12")))
	if err != nil {
		t.Fatalf("%v %+v", err, report.Checks)
	}
	seen := false
	for _, c := range report.Checks {
		seen = seen || (c.Name == "migration registry" && c.OK)
	}
	if !seen || report.SchemaVersion < 1 {
		t.Fatalf("registry check missing: %+v", report)
	}
}
