package pricing_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func seed(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../../apps/client/src/features/pricing/fixtures/approved.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type queryStub struct {
	active             db.ActivePricingRow
	next               db.NextPricingRow
	activeErr, nextErr error
}

func (q queryStub) ActivePricing(context.Context) (db.ActivePricingRow, error) {
	return q.active, q.activeErr
}
func (q queryStub) NextPricing(context.Context) (db.NextPricingRow, error) { return q.next, q.nextErr }

func TestVersionedDTOAndConditionalRead(t *testing.T) {
	raw := seed(t)
	c, err := pricing.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := queryStub{active: db.ActivePricingRow{Version: c.Version, EffectiveFrom: pgtype.Timestamptz{Time: c.EffectiveFrom, Valid: true}, Config: raw}, nextErr: pgx.ErrNoRows}
	catalog, err := pricing.Load(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	catalog.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), "GET", "/v1/pricing", nil))
	if recorder.Code != 200 || recorder.Header().Get("ETag") == "" {
		t.Fatal("missing versioned response")
	}
	for _, tag := range []string{recorder.Header().Get("ETag"), "W/" + recorder.Header().Get("ETag"), `"other", ` + recorder.Header().Get("ETag"), "*"} {
		request := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/pricing", nil)
		request.Header.Set("If-None-Match", tag)
		r := httptest.NewRecorder()
		catalog.ServeHTTP(r, request)
		if r.Code != 304 || r.Body.Len() != 0 {
			t.Fatalf("conditional GET: %d %s", r.Code, r.Body.String())
		}
	}
	cloned := catalog.Config()
	cloned.Actions["preview"] = pricing.Action{Credits: 0}
	if catalog.Config().Actions["preview"].Credits == 0 {
		t.Fatal("mutable catalogue exposed")
	}
	for _, bad := range []queryStub{{activeErr: pgx.ErrNoRows}, {active: q.active, nextErr: errors.New("database unavailable")}, {active: db.ActivePricingRow{Version: "wrong", Config: raw}}, {active: db.ActivePricingRow{Config: []byte(`{}`)}}} {
		if _, err := pricing.Load(t.Context(), bad); err == nil {
			t.Fatal("invalid boot accepted")
		}
	}
	q.nextErr = nil
	q.next = db.NextPricingRow{Version: c.Version, EffectiveFrom: q.active.EffectiveFrom, Config: raw}
	scheduled, err := pricing.Load(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	scheduled.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", "/v1/pricing", nil))
	if !strings.Contains(r.Body.String(), `"next_scheduled":{"version"`) {
		t.Fatal("scheduled version absent")
	}
}

func TestSchemaAndCrossFieldValidation(t *testing.T) {
	raw := seed(t)
	if _, err := pricing.Parse(raw); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"negative", func(c map[string]any) { c["daily_free"].(map[string]any)["credits"] = -1 }},
		{"fractional", func(c map[string]any) { c["daily_free"].(map[string]any)["credits"] = 1.5 }},
		{"unknown action", func(c map[string]any) { c["actions"].(map[string]any)["invented"] = map[string]any{"credits": 1} }},
		{"absent action", func(c map[string]any) { delete(c["actions"].(map[string]any), "outline") }},
		{"zero denominator", func(c map[string]any) { c["narration"].(map[string]any)["characters"] = 0 }},
		{"inconsistent narration", func(c map[string]any) { c["narration"].(map[string]any)["credits"] = 1 }},
		{"unknown eligibility", func(c map[string]any) { c["daily_free"].(map[string]any)["eligible_actions"] = []string{"invented"} }},
		{"missing annual", func(c map[string]any) { delete(c["plans"].([]any)[0].(map[string]any), "yearly") }},
		{"duplicate sku", func(c map[string]any) {
			ps := c["packs"].([]any)
			ps[1].(map[string]any)["sku"] = ps[0].(map[string]any)["sku"]
		}},
		{"currency mismatch", func(c map[string]any) { c["packs"].([]any)[0].(map[string]any)["currency"] = "EUR" }},
		{"missing mappings", func(c map[string]any) { c["development_mappings"] = map[string]any{} }},
		{"real provider id", func(c map[string]any) { c["development_mappings"].(map[string]any)["pack_try"] = "price_invented" }},
		{"unknown field", func(c map[string]any) { c["secret_key"] = "private" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c map[string]any
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Fatal(err)
			}
			tc.mutate(c)
			bad, _ := json.Marshal(c)
			if _, err := pricing.Parse(bad); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestDevelopmentMappingsAndSafeDTO(t *testing.T) {
	c, err := pricing.Parse(seed(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ProviderMappings("development")) != 9 {
		t.Fatal("development mappings absent")
	}
	for _, env := range []string{"production", "staging", "test", ""} {
		if len(c.ProviderMappings(env)) != 0 {
			t.Fatal("synthetic mappings leaked")
		}
	}
	raw, err := json.Marshal(c.Public())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "synthetic:") || !strings.Contains(string(raw), `"sales_enabled":false`) {
		t.Fatal("unsafe DTO")
	}
}
