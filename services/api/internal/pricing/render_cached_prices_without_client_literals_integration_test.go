//go:build integration

package pricing_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestConditionalPricingReadRevalidatesAcrossVersionFlip(t *testing.T) {
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
	if err := migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}

	config, err := pricing.Parse(seed(t))
	if err != nil {
		t.Fatal(err)
	}
	scheduledAt := time.Now().UTC().Add(1200 * time.Millisecond).Truncate(time.Microsecond)
	config.Version = "e12-03-flip-fixture"
	config.EffectiveFrom = scheduledAt
	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_versions(version, effective_from, approved, config) VALUES ($1, $2, true, $3)", config.Version, scheduledAt, configJSON); err != nil {
		t.Fatal(err)
	}

	before, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	if before.Config().Version != "usd-quotes-2026-11-09" {
		t.Fatalf("current version before flip=%s", before.Config().Version)
	}
	body := httptest.NewRecorder()
	before.ServeHTTP(body, httptest.NewRequestWithContext(ctx, "GET", "/v1/pricing", nil))
	oldETag := body.Header().Get("ETag")
	if body.Code != 200 || oldETag == "" || !strings.Contains(body.Body.String(), `"next_scheduled":{"version":"e12-03-flip-fixture"`) {
		t.Fatalf("current version or next scheduled DTO missing: status=%d body=%s", body.Code, body.Body.String())
	}
	conditional := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(ctx, "GET", "/v1/pricing", nil)
	request.Header.Set("If-None-Match", oldETag)
	before.ServeHTTP(conditional, request)
	if conditional.Code != 304 || conditional.Body.Len() != 0 {
		t.Fatalf("unchanged version revalidation: status=%d body=%q", conditional.Code, conditional.Body.String())
	}

	if delay := time.Until(scheduledAt.Add(20 * time.Millisecond)); delay > 0 {
		time.Sleep(delay)
	}
	after, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	if after.Config().Version != config.Version {
		t.Fatalf("active version after flip=%s want=%s", after.Config().Version, config.Version)
	}
	changed := httptest.NewRecorder()
	request = httptest.NewRequestWithContext(ctx, "GET", "/v1/pricing", nil)
	request.Header.Set("If-None-Match", oldETag)
	after.ServeHTTP(changed, request)
	if changed.Code != 200 || changed.Header().Get("ETag") == oldETag || !strings.Contains(changed.Body.String(), `"version":"e12-03-flip-fixture"`) {
		t.Fatalf("old validator did not receive changed catalogue: status=%d body=%s", changed.Code, changed.Body.String())
	}
	if !strings.Contains(changed.Body.String(), `"currency":"USD"`) || !strings.Contains(changed.Body.String(), `"amount_minor":999`) {
		t.Fatal("flipped public offer lost currency/amount fields used by the shared formatter")
	}
	current := httptest.NewRecorder()
	request = httptest.NewRequestWithContext(ctx, "GET", "/v1/pricing", nil)
	request.Header.Set("If-None-Match", changed.Header().Get("ETag"))
	after.ServeHTTP(current, request)
	if current.Code != 304 || current.Body.Len() != 0 {
		t.Fatalf("new version revalidation: status=%d body=%q", current.Code, current.Body.String())
	}
}
