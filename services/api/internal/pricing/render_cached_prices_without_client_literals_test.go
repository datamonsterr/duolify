package pricing_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestRenderPricingConditionalReadBoundaries(t *testing.T) {
	raw := seed(t)
	config, err := pricing.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	query := queryStub{
		active: db.ActivePricingRow{
			Version:       config.Version,
			EffectiveFrom: pgtype.Timestamptz{Time: config.EffectiveFrom, Valid: true},
			Config:        raw,
		},
		nextErr: pgx.ErrNoRows,
	}
	catalogue, err := pricing.Load(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}

	initial := httptest.NewRecorder()
	catalogue.ServeHTTP(initial, httptest.NewRequestWithContext(t.Context(), "GET", "/v1/pricing", nil))
	if initial.Code != 200 || initial.Header().Get("ETag") == "" {
		t.Fatalf("initial read: status=%d etag=%q", initial.Code, initial.Header().Get("ETag"))
	}
	if !strings.Contains(initial.Body.String(), `"currency":"USD"`) || !strings.Contains(initial.Body.String(), `"amount_minor":999`) {
		t.Fatal("public offers omitted their server currency or integer amount")
	}

	for _, tc := range []struct {
		name       string
		validator  string
		wantStatus int
	}{
		{name: "missing validator", wantStatus: 200},
		{name: "different version", validator: `"old-version"`, wantStatus: 200},
		{name: "matching validator", validator: initial.Header().Get("ETag"), wantStatus: 304},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/pricing", nil)
			if tc.validator != "" {
				request.Header.Set("If-None-Match", tc.validator)
			}
			recorder := httptest.NewRecorder()
			catalogue.ServeHTTP(recorder, request)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d", recorder.Code, tc.wantStatus)
			}
			if tc.wantStatus == 304 && recorder.Body.Len() != 0 {
				t.Fatal("304 response unexpectedly contained a body")
			}
		})
	}
}
