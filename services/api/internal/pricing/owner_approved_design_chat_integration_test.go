//go:build integration

package pricing_test

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
)

func approveOwnerDesignChatFixture(t *testing.T, db *store.Store, admin *pricing.Admin, ownerID, authorID string) {
	t.Helper()
	ctx := t.Context()
	raw, err := os.ReadFile("fixtures/usd_design_chat_owner_approved.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	// Start from the pre-existing E16 action, keeping the approval diff scoped to
	// design chat. Seed only this test baseline, never the new approved version.
	baseRaw, err := os.ReadFile("fixtures/usd_outline_replan.json")
	if err != nil {
		t.Fatal(err)
	}
	var base map[string]any
	if err := json.Unmarshal(baseRaw, &base); err != nil {
		t.Fatal(err)
	}
	base["version"] = "usd-owner-replan-baseline"
	base["limits"].(map[string]any)["free_concurrent_generation"] = cfg["limits"].(map[string]any)["free_concurrent_generation"]
	base["effective_from"] = time.Now().UTC().Add(-time.Millisecond).Truncate(time.Microsecond).Format(time.RFC3339Nano)
	baseRaw, _ = json.Marshal(base)
	for _, statement := range []string{
		"INSERT INTO pricing_versions(version,effective_from,approved,config) VALUES('usd-owner-replan-baseline',($1::jsonb->>'effective_from')::timestamptz,true,$1::jsonb)",
		"INSERT INTO pricing_quote_policies(version,policy) SELECT 'usd-owner-replan-baseline',policy FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09' AND $1::jsonb IS NOT NULL",
		"INSERT INTO pricing_activation_events(version,activated_at) SELECT 'usd-owner-replan-baseline',now() WHERE $1::jsonb IS NOT NULL",
	} {
		if _, err := db.Pool.Exec(ctx, statement, baseRaw); err != nil {
			t.Fatal(err)
		}
	}
	admin.SetEnvironment("production")
	defer admin.SetEnvironment("development")
	cfg["effective_from"] = time.Now().UTC().Add(5 * time.Second).Truncate(time.Microsecond).Format(time.RFC3339Nano)
	version := cfg["version"].(string)
	call := func(actor, path string, payload any, want int) {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		status, out := admin.ExecuteCLI(ctx, actor, http.MethodPost, path, body)
		if status != want {
			t.Fatalf("%s: status=%d want=%d body=%s", path, status, want, out)
		}
	}
	call(authorID, "/admin/v1/pricing/drafts", map[string]any{"config": cfg, "reason": "ADR 0013 fixture"}, http.StatusCreated)
	path := "/admin/v1/pricing/drafts/" + version
	call(authorID, path+"/submit", map[string]string{"reason": "submit"}, http.StatusOK)
	call(authorID, path+"/approve", map[string]string{"reason": "non-owner"}, http.StatusForbidden)
	call(ownerID, path+"/approve", map[string]string{"reason": "owner approved"}, http.StatusOK)
	initial, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	runtime := pricing.NewRuntimeCatalogue(initial, db.Queries, db.Pool, nil)
	runtime.SetEnvironment("production")
	time.Sleep(5100 * time.Millisecond)
	if err := runtime.ActivateDue(ctx, admin); err != nil {
		t.Fatal(err)
	}
	active, err := pricing.Load(ctx, db.Queries)
	if err != nil || active.Config().Version != version {
		t.Fatalf("approved fixture did not activate: %v", err)
	}
	for _, action := range []string{"setup_chat", "course_brief_chat", "outline_chat_change", "outline_replan"} {
		if !runtime.HasAction(action) {
			t.Fatalf("action missing after owner approval: %s", action)
		}
	}
	var reviewer string
	var auditCount int
	if err := db.Pool.QueryRow(ctx, "SELECT reviewed_by, (SELECT count(*) FROM pricing_audit WHERE version=$1 AND action='version_approved') FROM pricing_drafts WHERE version=$1", version).Scan(&reviewer, &auditCount); err != nil || reviewer != ownerID || auditCount != 1 {
		t.Fatalf("owner approval audit: reviewer=%s count=%d err=%v", reviewer, auditCount, err)
	}
	for _, statement := range []string{"UPDATE pricing_versions SET config=config WHERE version=$1", "DELETE FROM pricing_versions WHERE version=$1"} {
		if _, err := db.Pool.Exec(ctx, statement, version); err == nil {
			t.Fatalf("approved version mutation allowed: %s", statement)
		}
	}
}
