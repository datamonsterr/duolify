//go:build integration

package pricing_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestSeedResolveRestoreAndDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := pricing.Load(ctx, db.Queries); err == nil {
		t.Fatal("boot accepted absent catalogue")
	}
	for range 2 {
		if err := migrations.Up(ctx, url); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	assertApproved(t, catalog.Config())
	recorder := httptest.NewRecorder()
	catalog.ServeHTTP(recorder, httptest.NewRequestWithContext(ctx, "GET", "/v1/pricing", nil))
	if recorder.Code != 200 || recorder.Header().Get("ETag") == "" || recorder.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("invalid response %v", recorder.Result())
	}
	if strings.Contains(recorder.Body.String(), "synthetic:") || !strings.Contains(recorder.Body.String(), `"sales_enabled":false`) {
		t.Fatal("unsafe public mapping/sales")
	}
	for _, sql := range []string{"UPDATE pricing_versions SET config='{}'", "DELETE FROM pricing_versions", "TRUNCATE pricing_versions"} {
		if _, err := db.Pool.Exec(ctx, sql); err == nil {
			t.Fatalf("accepted %s", sql)
		}
	}
	// Account deletion cannot cascade to the catalogue: no learner identity is held.
	if _, err := db.Pool.Exec(ctx, "INSERT INTO learners(display_name) VALUES ('deleted'); DELETE FROM learners"); err != nil {
		t.Fatal(err)
	}
	// Exercise a real custom-format nightly dump and restore, not a JSON copy.
	dump, _, err := container.Exec(ctx, []string{"pg_dump", "-U", "duolify", "-d", "duolify", "-Fc", "-f", "/tmp/pricing.dump"})
	if err != nil || dump != 0 {
		t.Fatalf("dump: %d %v", dump, err)
	}
	code, _, err := container.Exec(ctx, []string{"createdb", "-U", "duolify", "restored"})
	if err != nil || code != 0 {
		t.Fatalf("createdb: %d %v", code, err)
	}
	code, _, err = container.Exec(ctx, []string{"pg_restore", "-U", "duolify", "-d", "restored", "--exit-on-error", "/tmp/pricing.dump"})
	if err != nil || code != 0 {
		t.Fatalf("restore: %d %v", code, err)
	}
	restored, err := store.New(ctx, strings.Replace(url, "/duolify?", "/restored?", 1))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	after, err := pricing.Load(ctx, restored.Queries)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(catalog.Config(), after.Config()) {
		t.Fatal("restore resolver mismatch")
	}
	// Bypass the immutable trigger only to simulate corruption in a restored database.
	if _, err := restored.Pool.Exec(ctx, "ALTER TABLE pricing_versions DISABLE TRIGGER pricing_versions_immutable; UPDATE pricing_versions SET config='{}'"); err != nil {
		t.Fatal(err)
	}
	if _, err := pricing.Load(ctx, restored.Queries); err == nil {
		t.Fatal("boot accepted invalid config")
	}
}

func assertApproved(t *testing.T, c pricing.Config) {
	t.Helper()
	source, err := os.ReadFile("../../../../docs/business/monetization.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{"| Course preview | 25 |", "| Course outline | 55 |", "| Lesson content | 140 |", "| Narration synthesis | 50 for 1,440", "**46 Credits**", "| Try | 5 | 400 |", "| Build | 15 | 1,350 |", "| Deep | 40 | 4,000 |", "| Learn | 9.99 | 96 | 8 | 600 |", "| Plus | 19.99 | 192 | 16 | 1,600 |", "| Pro | 39.99 | 384 | 32 | 3,600 |"} {
		if !strings.Contains(string(source), row) {
			t.Fatalf("approved source table changed: %s", row)
		}
	}
	expected := map[string]int{"preview": 25, "outline": 55, "lesson_content": 140, "narration": 50, "tutor_short": 5, "tutor_long": 8, "quick_note": 2, "cached_replay": 0}
	for key, value := range expected {
		if c.Actions[key].Credits != value {
			t.Errorf("%s=%d want %d", key, c.Actions[key].Credits, value)
		}
	}
	if c.Version != "usd-launch-2026-11-08" || c.DailyFree.Credits != 46 || c.Actions["lesson_content"].Credits+c.Actions["narration"].Credits != 190 {
		t.Fatal("seed arithmetic drift")
	}
	want := []struct{ monthly, yearly, credits, parallel int }{{999, 9600, 600, 1}, {1999, 19200, 1600, 1}, {3999, 38400, 3600, 2}}
	for i, p := range c.Plans {
		w := want[i]
		if p.Monthly.AmountMinor != w.monthly || p.Yearly.AmountMinor != w.yearly || p.MonthlyCredits != w.credits || p.ConcurrentGeneration != w.parallel {
			t.Errorf("plan drift: %+v", p)
		}
	}
	for i, w := range []struct{ amount, credits int }{{500, 400}, {1500, 1350}, {4000, 4000}} {
		if c.Packs[i].AmountMinor != w.amount || c.Packs[i].Credits != w.credits {
			t.Fatal("pack drift")
		}
	}
	encoded, _ := json.Marshal(c)
	if !strings.Contains(string(encoded), "Learning history export") {
		t.Fatal("benefit missing")
	}
	expectedConfig, err := pricing.Parse(seed(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, expectedConfig) {
		t.Fatal("migration differs from complete approved catalogue fixture")
	}
}
