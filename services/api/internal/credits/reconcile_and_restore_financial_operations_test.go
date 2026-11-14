package credits

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/google/uuid"
)

func TestFinancialLiabilityIncludesHoldsAndRejectsReserveOverflow(t *testing.T) {
	var state FinancialState
	if err := addPaidSource(&state, 7, 3, 2); err != nil || state.PaidLiability != 10 || state.PaidHeld != 3 || state.ReserveNanos != 20 {
		t.Fatal(state, err)
	}
	for _, tc := range [][3]int64{{-1, 0, 1}, {1, -1, 1}, {1, 0, 0}, {math.MaxInt64, 1, 1}, {math.MaxInt64, 0, 2}, {math.MaxInt64 - 10, 0, 1}} {
		candidate := state
		if err := addPaidSource(&candidate, tc[0], tc[1], tc[2]); err == nil {
			t.Fatal("invalid financial reserve accepted", tc)
		}
		if candidate != state {
			t.Fatal("failed reserve update mutated financial state")
		}
	}
}

func TestFinancialInboxBackoffAndObservationKeys(t *testing.T) {
	for _, tc := range []struct {
		attempt int
		delay   time.Duration
	}{{1, time.Minute}, {2, 2 * time.Minute}, {5, 16 * time.Minute}, {20, time.Hour}} {
		if got := inboxRetryDelay(tc.attempt); got != tc.delay {
			t.Fatalf("attempt %d delay %v", tc.attempt, got)
		}
	}
	a := financialObservationID("session", "cs_one", []byte(`{"paid":true}`))
	if a != financialObservationID("session", "cs_one", []byte(`{"paid":true}`)) || a == financialObservationID("session", "cs_one", []byte(`{"paid":false}`)) || a == financialObservationID("invoice", "cs_one", []byte(`{"paid":true}`)) || !paymentIdentifier.MatchString(a) {
		t.Fatal("observation keys are not stable and content addressed")
	}
}

func TestSignedFinancialFixturesRejectUnsignedAndLiveState(t *testing.T) {
	raw := []byte(`{"mode":"test","sessions":[{"ID":"cs_fixture","OrderID":"order","PaymentStatus":"paid"}]}`)
	sign := func(raw []byte) string {
		mac := hmac.New(sha256.New, []byte("fixture-secret"))
		_, _ = mac.Write(raw)
		return hex.EncodeToString(mac.Sum(nil))
	}
	if _, err := LoadSignedFinancialFixtures(raw, sign(raw), "fixture-secret"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		raw         []byte
		sig, secret string
	}{{raw, "", "fixture-secret"}, {raw, sign(raw), "wrong"}, {raw, sign(raw), ""}, {append(bytes.Clone(raw), ' '), sign(raw), "fixture-secret"}} {
		if _, err := LoadSignedFinancialFixtures(tc.raw, tc.sig, tc.secret); err == nil {
			t.Fatal("unverified fixture accepted")
		}
	}
	for _, raw := range [][]byte{[]byte(`{"mode":"live"}`), []byte(`{"mode":"test","invoices":[{"LiveMode":true}]}`), []byte(`{"mode":"test","sessions":[{"LiveMode":true}]}`)} {
		if _, err := LoadSignedFinancialFixtures(raw, sign(raw), "fixture-secret"); err == nil {
			t.Fatal("live fixture accepted")
		}
	}
}

func TestPaymentReconciliationFailureDeliveryPrivacyOptOutAndIndependentAlert(t *testing.T) {
	for _, optOut := range []bool{false, true} {
		t.Run(map[bool]string{false: "delivery", true: "opt-out"}[optOut], func(t *testing.T) {
			var mu sync.Mutex
			var captured []struct {
				Event      string         `json:"event"`
				DistinctID string         `json:"distinct_id"`
				Properties map[string]any `json:"properties"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var batch struct {
					Batch []json.RawMessage `json:"batch"`
				}
				if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
					t.Error(err)
				}
				mu.Lock()
				defer mu.Unlock()
				for _, raw := range batch.Batch {
					var e struct {
						Event      string         `json:"event"`
						DistinctID string         `json:"distinct_id"`
						Properties map[string]any `json:"properties"`
					}
					if err := json.Unmarshal(raw, &e); err != nil {
						t.Error(err)
					}
					captured = append(captured, e)
				}
				w.WriteHeader(200)
			}))
			defer server.Close()
			events, err := analytics.NewWithOptions("test-only", server.URL, analytics.Options{OptOut: optOut, Consent: analytics.DatabaseConsent(nil)})
			if err != nil {
				t.Fatal(err)
			}
			monitor := telemetry.New(slog.New(slog.NewJSONHandler(io.Discard, nil)))
			ctx, end := monitor.Start(t.Context(), "job")
			defer end(false)
			s := NewServiceWithCatalogue(nil, nil, nil, events)
			s.UseOperationalMonitor(monitor)
			s.reconciliationFailure(ctx, "private learner text and provider error")
			if err = events.Close(); err != nil {
				t.Fatal(err)
			}
			metrics := httptest.NewRecorder()
			monitor.ServeHTTP(metrics, httptest.NewRequestWithContext(ctx, "GET", "/metrics", nil))
			if !strings.Contains(metrics.Body.String(), `duolify_alerts_total{kind="dependency"} 1`) {
				t.Fatal("missing independent operational alert")
			}
			mu.Lock()
			defer mu.Unlock()
			if optOut {
				if len(captured) != 0 {
					t.Fatal("opt-out delivered events")
				}
				return
			}
			if len(captured) != 1 {
				t.Fatalf("delivered %d events", len(captured))
			}
			e := captured[0]
			if _, err := uuid.Parse(e.DistinctID); err != nil || e.DistinctID != "00000000-0000-4000-8000-000000000011" || e.Event != "payment_reconciliation_failed" || e.Properties["source"] != "api" || e.Properties["platform"] != "web" || e.Properties["app_version"] == "" || e.Properties["reason_code"] != "mismatch" || e.Properties["channel"] != "stripe" {
				t.Fatalf("invalid failure event %+v", e)
			}
			raw, _ := json.Marshal(e)
			if bytes.Contains(raw, []byte("private")) || bytes.Contains(raw, []byte("learner")) {
				t.Fatal("private diagnostic leaked")
			}
		})
	}
}
