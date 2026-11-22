//go:build integration

package pricing_test

import (
	"encoding/json"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"reflect"
	"testing"
)

func TestCheckInPricingAddsOnlyOwnerApprovedGrant(t *testing.T) {
	ctx := t.Context()
	pg, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("attendance_prices"), postgres.WithUsername("test"), postgres.WithPassword("test"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, pg)
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	active, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	cfg := active.Config()
	if cfg.CheckIn == nil || cfg.CheckIn.DailyCredits != 150 || cfg.CheckIn.FirstTimeBonus != 250 || cfg.CheckIn.BankDays != 7 {
		t.Fatalf("check-in config: %+v", cfg.CheckIn)
	}
	var parent, child []byte
	var samePolicy bool
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-hy4-routing-2026-11-22' AND approved").Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-hy4-check-in-2026-11-22' AND approved").Scan(&child); err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	if err = json.Unmarshal(parent, &a); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(child, &b); err != nil {
		t.Fatal(err)
	}
	delete(a, "version")
	delete(b, "version")
	delete(a, "effective_from")
	delete(b, "effective_from")
	delete(b, "check_in")
	if !reflect.DeepEqual(a, b) {
		t.Fatal("check-in migration changed other prices, packs, plans or limits")
	}
	if err = db.Pool.QueryRow(ctx, `SELECT (SELECT policy FROM pricing_quote_policies WHERE version=$1)=(SELECT policy FROM pricing_quote_policies WHERE version='usd-hy4-routing-2026-11-22')`, cfg.Version).Scan(&samePolicy); err != nil || !samePolicy {
		t.Fatalf("routing changed: %v", err)
	}
	var current []byte
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version=$1 AND approved", cfg.Version).Scan(&current); err != nil {
		t.Fatal(err)
	}
	var base, next map[string]any
	if err = json.Unmarshal(child, &base); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(current, &next); err != nil {
		t.Fatal(err)
	}
	if next["version"] != activeVersion {
		t.Fatalf("unexpected active version: %v", next["version"])
	}
	transcribe, ok := next["transcribe"].(map[string]any)
	if !ok || transcribe["credits"] != float64(0) || transcribe["usd_nanos_per_second"] != float64(3330) || transcribe["max_cost_nanos"] != float64(500000) || transcribe["daily_requests"] != float64(60) {
		t.Fatalf("transcription pricing: %v", next["transcribe"])
	}
	basePreview := base["limits"].(map[string]any)["preview"].(map[string]any)
	nextPreview := next["limits"].(map[string]any)["preview"].(map[string]any)
	if nextPreview["output_tokens"] != float64(6000) {
		t.Fatalf("preview cap: %v", nextPreview)
	}
	basePreview["output_tokens"] = float64(6000)
	delete(next, "transcribe")
	delete(base, "version")
	delete(next, "version")
	delete(base, "effective_from")
	delete(next, "effective_from")
	if !reflect.DeepEqual(base, next) {
		t.Fatal("voice pricing migration changed unrelated terms")
	}
}
