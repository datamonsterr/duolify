package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
)

// Shares the real authenticated HTTP server and disposable database with E12-10.
func testOwnerApprovedDesignChatHTTP(t *testing.T, db *store.Store, admin *pricing.Admin, runtime *pricing.RuntimeCatalogue, quotes *pricing.Quotes, request func(string, string, string, []byte) *httptest.ResponseRecorder, authorToken, nonOwnerToken, ownerToken, ownerID string) {
	t.Helper()
	ctx := t.Context()
	raw, err := os.ReadFile("../../internal/pricing/fixtures/usd_design_chat_owner_approved.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	version := cfg["version"].(string)
	// Model an existing E16 catalogue. Production rollout must preserve this
	// separate action; the new design chat fixture must not remove it.
	baseRaw, err := os.ReadFile("../../internal/pricing/fixtures/usd_outline_replan.json")
	if err != nil {
		t.Fatal(err)
	}
	var base map[string]any
	if err := json.Unmarshal(baseRaw, &base); err != nil {
		t.Fatal(err)
	}
	base["limits"].(map[string]any)["free_concurrent_generation"] = cfg["limits"].(map[string]any)["free_concurrent_generation"]
	base["effective_from"] = time.Now().UTC().Add(-time.Millisecond).Truncate(time.Microsecond).Format(time.RFC3339Nano)
	baseRaw, _ = json.Marshal(base)
	if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_versions(version,effective_from,approved,config) VALUES($1,$2,true,$3)", base["version"], base["effective_from"], baseRaw); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_quote_policies(version,policy) SELECT $1,policy FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09'", base["version"]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_activation_events(version,activated_at) VALUES($1,now())", base["version"]); err != nil {
		t.Fatal(err)
	}
	admin.SetEnvironment("production")
	runtime.SetEnvironment("production")
	quotes.SetEnvironment("production")
	defer admin.SetEnvironment("development")
	defer runtime.SetEnvironment("development")
	defer quotes.SetEnvironment("development")
	// The fixture contains owner decision metadata; the actual reviewer is still
	// required by production quoting as well as approval and activation.
	quotes.SetOwnerID(ownerID)
	call := func(path, token string, payload any, want int) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		w := request(http.MethodPost, path, token, body)
		if w.Code != want {
			t.Fatalf("%s: status=%d want=%d body=%s", path, w.Code, want, w.Body)
		}
		return w
	}
	for _, decision := range []string{"", "pending", "interim_adr_0012", "owner_approved"} {
		candidate := make(map[string]any)
		for k, v := range cfg {
			candidate[k] = v
		}
		candidate["version"] = version + "-rejected-" + strings.ReplaceAll(decision, "_", "-")
		candidate["effective_from"] = time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
		if decision == "" {
			delete(candidate, "design_chat_decision")
		} else {
			candidate["design_chat_decision"] = decision
		}
		path := "/admin/v1/pricing/drafts/" + candidate["version"].(string)
		call("/admin/v1/pricing/drafts", authorToken, map[string]any{"config": candidate, "reason": "decision gate fixture"}, http.StatusCreated)
		call(path+"/submit", authorToken, map[string]string{"reason": "submit"}, http.StatusOK)
		token := ownerToken
		if decision == "owner_approved" {
			token = nonOwnerToken
		}
		call(path+"/approve", token, map[string]string{"reason": "must be rejected"}, http.StatusForbidden)
	}
	// Fixture date is illustrative; approval always requires an explicit future
	// schedule. Tests select one only in their disposable database.
	cfg["effective_from"] = time.Now().UTC().Add(5 * time.Second).Truncate(time.Microsecond).Format(time.RFC3339Nano)
	path := "/admin/v1/pricing/drafts/" + version
	call("/admin/v1/pricing/drafts", authorToken, map[string]any{"config": cfg, "reason": "ADR 0013 owner decision"}, http.StatusCreated)
	call(path+"/submit", authorToken, map[string]string{"reason": "submit"}, http.StatusOK)
	call(path+"/schedule", ownerToken, map[string]string{"reason": "owner approved schedule"}, http.StatusOK)
	time.Sleep(5100 * time.Millisecond)
	if err := runtime.ActivateDue(ctx, admin); err != nil {
		t.Fatal(err)
	}
	w := request(http.MethodGet, "/v1/pricing", "", nil)
	var active pricing.PublicConfig
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &active) != nil || active.Version != version {
		t.Fatalf("active pricing: %d %s", w.Code, w.Body)
	}
	for _, action := range []string{"setup_chat", "course_brief_chat", "outline_chat_change"} {
		if !runtime.HasAction(action) {
			t.Fatalf("production action disabled: %s", action)
		}
	}
	if active.SalesEnabled || active.Actions["outline_replan"].Credits != 55 {
		t.Fatalf("unrelated catalogue changed: %+v", active)
	}
	w = call("/v1/quotes", authorToken, map[string]string{"action": "setup_chat"}, http.StatusOK)
	var quote pricing.Quote
	if err := json.Unmarshal(w.Body.Bytes(), &quote); err != nil || quote.PricingVersion != version || quote.ContentCeiling != 0 {
		t.Fatalf("production quote: %s (%v)", w.Body, err)
	}
	rollbackVersion := version + "-rollback"
	call("/admin/v1/pricing/drafts/"+base["version"].(string)+"/rollback", authorToken, map[string]any{"version": rollbackVersion, "effective_from": time.Now().UTC().Add(5 * time.Second).Truncate(time.Microsecond).Format(time.RFC3339Nano), "reason": "disable design chat only"}, http.StatusCreated)
	rollbackPath := "/admin/v1/pricing/drafts/" + rollbackVersion
	call(rollbackPath+"/submit", authorToken, map[string]string{"reason": "submit rollback"}, http.StatusOK)
	call(rollbackPath+"/approve", nonOwnerToken, map[string]string{"reason": "approve rollback"}, http.StatusOK)
	time.Sleep(5100 * time.Millisecond)
	if err := runtime.ActivateDue(ctx, admin); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"setup_chat", "course_brief_chat", "outline_chat_change"} {
		if runtime.HasAction(action) {
			t.Fatalf("rollback left %s enabled", action)
		}
	}
	w = request(http.MethodGet, "/v1/pricing", "", nil)
	var rollback pricing.PublicConfig
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &rollback) != nil {
		t.Fatalf("rollback pricing: %d %s", w.Code, w.Body)
	}
	for key, action := range active.Actions {
		if !pricing.IsDesignChatAction(key) && !reflect.DeepEqual(action, rollback.Actions[key]) {
			t.Fatalf("rollback changed unrelated action %s", key)
		}
	}
	w = request(http.MethodGet, "/v1/quotes/"+quote.ID, authorToken, nil)
	var retained pricing.Quote
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &retained) != nil || !reflect.DeepEqual(quote, retained) {
		t.Fatalf("rollback repriced historical quote: %d %s", w.Code, w.Body)
	}
}
