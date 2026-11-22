package pricing_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type failingPricingQueries struct{ activeErr, nextErr error }

func (q failingPricingQueries) ActivePricing(context.Context) (db.ActivePricingRow, error) {
	return db.ActivePricingRow{}, q.activeErr
}
func (q failingPricingQueries) NextPricing(context.Context) (db.NextPricingRow, error) {
	return db.NextPricingRow{}, q.nextErr
}

func TestScheduleAndRollbackRuntimeKeepsLastKnownGoodOnReloadFailure(t *testing.T) {
	raw := seed(t)
	cfg, err := pricing.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := pricing.Load(t.Context(), queryStub{active: db.ActivePricingRow{Version: cfg.Version, EffectiveFrom: pgtype.Timestamptz{Time: cfg.EffectiveFrom, Valid: true}, Config: raw}, nextErr: pgx.ErrNoRows})
	if err != nil {
		t.Fatal(err)
	}
	runtime := pricing.NewRuntimeCatalogue(initial, failingPricingQueries{activeErr: errors.New("postgres unavailable")}, nil, nil)
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/pricing", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("reload failure status=%d", response.Code)
	}
	if got := response.Body.String(); !containsVersion(got, cfg.Version) {
		t.Fatalf("last known good missing: %s", got)
	}
}

func containsVersion(body, version string) bool {
	return len(body) > 0 && strings.Contains(body, `"version":"`+version+`"`)
}

type alertRecorder struct {
	mu     sync.Mutex
	scopes []string
}

func (a *alertRecorder) raise(_ context.Context, scope string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.scopes = append(a.scopes, scope)
}

func (a *alertRecorder) has(scope string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Contains(a.scopes, scope)
}

func initialCatalogue(t *testing.T) *pricing.Catalogue {
	t.Helper()
	raw := seed(t)
	cfg, err := pricing.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := pricing.Load(t.Context(), queryStub{active: db.ActivePricingRow{Version: cfg.Version, EffectiveFrom: pgtype.Timestamptz{Time: cfg.EffectiveFrom, Valid: true}, Config: raw}, nextErr: pgx.ErrNoRows})
	if err != nil {
		t.Fatal(err)
	}
	return initial
}

func TestPricingConfigLoadFailureRaisesOperationalAlert(t *testing.T) {
	runtime := pricing.NewRuntimeCatalogue(initialCatalogue(t), failingPricingQueries{activeErr: errors.New("postgres unavailable")}, nil, nil)
	alerts := new(alertRecorder)
	runtime.SetAlertSink(alerts.raise)
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/pricing", nil))
	if response.Code != http.StatusOK || !alerts.has("pricing_config_load_failed") {
		t.Fatalf("status=%d alerts=%v", response.Code, alerts.scopes)
	}
}

func TestPricingScheduledFlipFailureRaisesOperationalAlert(t *testing.T) {
	runtime := pricing.NewRuntimeCatalogue(initialCatalogue(t), queryStub{}, nil, nil)
	alerts := new(alertRecorder)
	runtime.SetAlertSink(alerts.raise)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	runtime.RunActivator(ctx, nil) // no verifier, so ActivateDue refuses
	if !alerts.has("pricing_scheduled_flip_failed") {
		t.Fatalf("alerts=%v", alerts.scopes)
	}
}
