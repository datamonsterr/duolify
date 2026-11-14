//go:build integration

package pricing_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestQuoteResolverActivation(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("quote_resolver_activation"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var owner string
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('quote activation learner') RETURNING id::text").Scan(&owner); err != nil {
		t.Fatal(err)
	}

	initial, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	runtime := pricing.NewRuntimeCatalogue(initial, db.Queries, db.Pool, nil)
	quotes := pricing.NewQuotes(db.Pool)

	readCatalogue := func() map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		runtime.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/pricing", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("catalogue GET status=%d body=%s", w.Code, w.Body)
		}
		var res map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}

	// Baseline: quote resolver version must equal public runtime catalogue version.
	catInitial := readCatalogue()
	q0, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if q0.PricingVersion != catInitial["version"] {
		t.Fatalf("initial mismatch: quote version=%q, catalogue version=%v", q0.PricingVersion, catInitial["version"])
	}
	baselineVersion := q0.PricingVersion

	// 1) Approved due version without activation marker -> quote uses previous activated version.
	var raw []byte
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version=$1", baselineVersion).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	unactivatedVersion := "usd-unactivated-successor"
	effectivePast := time.Now().Add(-5 * time.Minute).UTC().Truncate(time.Microsecond)
	config["version"] = unactivatedVersion
	config["effective_from"] = effectivePast.Format(time.RFC3339Nano)
	config["actions"].(map[string]any)["preview"].(map[string]any)["credits"] = float64(22)
	encoded, _ := json.Marshal(config)

	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_versions(version,effective_from,approved,config) VALUES($1,$2,true,$3)", unactivatedVersion, effectivePast, encoded); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_quote_policies(version,policy) SELECT $1,policy FROM pricing_quote_policies WHERE version=$2", unactivatedVersion, baselineVersion); err != nil {
		t.Fatal(err)
	}

	// Without an activation marker, quote resolver must keep using previous activated version.
	q1, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if q1.PricingVersion != baselineVersion {
		t.Fatalf("unactivated version was quoted: got %q, want previous activated %q", q1.PricingVersion, baselineVersion)
	}
	catAfterUnactivated := readCatalogue()
	if catAfterUnactivated["version"] != baselineVersion {
		t.Fatalf("catalogue exposed unactivated version: got %v, want %q", catAfterUnactivated["version"], baselineVersion)
	}
	if q1.PricingVersion != catAfterUnactivated["version"] {
		t.Fatalf("quote version %q != catalogue version %v", q1.PricingVersion, catAfterUnactivated["version"])
	}

	// 2) After marker -> uses new one.
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_activation_events(version,activated_at) VALUES($1,$2)", unactivatedVersion, effectivePast); err != nil {
		t.Fatal(err)
	}

	q2, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if q2.PricingVersion != unactivatedVersion {
		t.Fatalf("quote did not use activated version: got %q, want %q", q2.PricingVersion, unactivatedVersion)
	}
	if q2.ContentCeiling != 22 {
		t.Fatalf("quote did not use updated price terms: got ceiling %d, want 22", q2.ContentCeiling)
	}
	catAfterActivation := readCatalogue()
	if catAfterActivation["version"] != unactivatedVersion {
		t.Fatalf("catalogue did not flip to activated version: got %v, want %q", catAfterActivation["version"], unactivatedVersion)
	}
	if q2.PricingVersion != catAfterActivation["version"] {
		t.Fatalf("quote version %q != catalogue version %v", q2.PricingVersion, catAfterActivation["version"])
	}

	// 3) Cancelled -> never quotable (falls back to previous activated version).
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_version_cancellations(version,cancelled_by,reason) VALUES($1,'test-admin','emergency cancellation')", unactivatedVersion); err != nil {
		t.Fatal(err)
	}

	q3, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if q3.PricingVersion == unactivatedVersion {
		t.Fatalf("cancelled version was quoted: %q", q3.PricingVersion)
	}
	if q3.PricingVersion != baselineVersion {
		t.Fatalf("quote did not fall back to previous activated version: got %q, want %q", q3.PricingVersion, baselineVersion)
	}
	catAfterCancellation := readCatalogue()
	if catAfterCancellation["version"] != baselineVersion {
		t.Fatalf("catalogue did not fall back after cancellation: got %v, want %q", catAfterCancellation["version"], baselineVersion)
	}
	if q3.PricingVersion != catAfterCancellation["version"] {
		t.Fatalf("quote version %q != catalogue version %v", q3.PricingVersion, catAfterCancellation["version"])
	}
}
