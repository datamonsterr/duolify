package pricing_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// countingQueries counts every statement the runtime catalogue issues and lets a test swap the
// stored version or break the database.
type countingQueries struct {
	statements atomic.Int64
	mu         sync.Mutex
	row        db.ActivePricingRow
	err        error
}

func (q *countingQueries) ActivePricing(context.Context) (db.ActivePricingRow, error) {
	q.statements.Add(1)
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.row, q.err
}

func (q *countingQueries) NextPricing(context.Context) (db.NextPricingRow, error) {
	q.statements.Add(1)
	return db.NextPricingRow{}, pgx.ErrNoRows
}

func (q *countingQueries) set(row db.ActivePricingRow, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.row, q.err = row, err
}

func snapshotFixture(t *testing.T) (*countingQueries, *pricing.RuntimeCatalogue, db.ActivePricingRow) {
	t.Helper()
	raw := seed(t)
	cfg, err := pricing.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	row := db.ActivePricingRow{Version: cfg.Version, EffectiveFrom: pgtype.Timestamptz{Time: cfg.EffectiveFrom, Valid: true}, Config: raw}
	q := &countingQueries{row: row}
	initial, err := pricing.Load(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	q.statements.Store(0)
	return q, pricing.NewRuntimeCatalogue(initial, q, nil, nil), row
}

func getPricing(t *testing.T, h http.Handler, etag string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/pricing", nil)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestWarmPricingRequestRunsZeroStatements(t *testing.T) {
	q, runtime, _ := snapshotFixture(t)
	first := getPricing(t, runtime, "")
	if first.Code != http.StatusOK {
		t.Fatalf("status=%d", first.Code)
	}
	cold := q.statements.Load()
	if cold == 0 {
		t.Fatal("the first request must load the snapshot from Postgres")
	}
	etag := first.Header().Get("ETag")
	for range 200 {
		if w := getPricing(t, runtime, ""); w.Code != http.StatusOK || w.Body.String() != first.Body.String() {
			t.Fatalf("warm read status=%d", w.Code)
		}
	}
	if w := getPricing(t, runtime, etag); w.Code != http.StatusNotModified {
		t.Fatalf("conditional read status=%d", w.Code)
	}
	if got := q.statements.Load(); got != cold {
		t.Fatalf("warm requests ran %d extra statements", got-cold)
	}
	if cc := first.Header().Get("Cache-Control"); cc != "public, max-age=10" || etag == "" {
		t.Fatalf("Cache-Control=%q ETag=%q", cc, etag)
	}
}

func TestPricingStalenessIsBoundedByTheTTL(t *testing.T) {
	q, runtime, row := snapshotFixture(t)
	runtime.SetSnapshotTTL(60 * time.Millisecond)
	getPricing(t, runtime, "")
	next := row
	next.Version = "usd-next"
	next.Config = []byte(strings.Replace(string(row.Config), `"`+row.Version+`"`, `"usd-next"`, 1))
	q.set(next, nil)
	if w := getPricing(t, runtime, ""); !containsVersion(w.Body.String(), row.Version) {
		t.Fatalf("a database change inside the TTL must not be served yet: %s", w.Body)
	}
	time.Sleep(90 * time.Millisecond)
	if w := getPricing(t, runtime, ""); !containsVersion(w.Body.String(), "usd-next") {
		t.Fatalf("a database change must show after the TTL: %s", w.Body)
	}
}

func TestExpiredSnapshotIsReloadedOnceUnderConcurrency(t *testing.T) {
	q, runtime, _ := snapshotFixture(t)
	runtime.SetSnapshotTTL(200 * time.Millisecond)
	getPricing(t, runtime, "")
	time.Sleep(250 * time.Millisecond)
	q.statements.Store(0)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if w := getPricing(t, runtime, ""); w.Code != http.StatusOK {
				t.Errorf("status=%d", w.Code)
			}
		})
	}
	wg.Wait()
	if got := q.statements.Load(); got != 2 {
		t.Fatalf("concurrent expired reads ran %d statements; reloads must be single-flight", got)
	}
}

func TestFailedRefreshKeepsLastGoodAlertsAndBacksOff(t *testing.T) {
	q, runtime, row := snapshotFixture(t)
	runtime.SetSnapshotTTL(80 * time.Millisecond)
	alerts := new(alertRecorder)
	runtime.SetAlertSink(alerts.raise)
	getPricing(t, runtime, "")
	q.set(db.ActivePricingRow{}, errors.New("postgres unavailable"))
	time.Sleep(100 * time.Millisecond)
	q.statements.Store(0)
	for range 50 {
		w := getPricing(t, runtime, "")
		if w.Code != http.StatusOK || !containsVersion(w.Body.String(), row.Version) {
			t.Fatalf("last good catalogue not served: status=%d", w.Code)
		}
	}
	if !alerts.has("pricing_config_load_failed") {
		t.Fatalf("alerts=%v", alerts.scopes)
	}
	if got := q.statements.Load(); got != 1 {
		t.Fatalf("a failing database was queried %d times in one TTL; want 1", got)
	}
}
