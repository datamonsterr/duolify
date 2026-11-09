//go:build integration

package pricing_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPricingAdminAuditRolesAndVersionImmutability(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("pricing_admin"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	var actor string
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('pricing admin fixture') RETURNING id::text").Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", actor); err != nil {
		t.Fatal(err)
	}
	admin := pricing.NewAdmin(db.Pool, nil, actor)
	var current map[string]any
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-launch-2026-11-08'").Scan(&current); err != nil {
		t.Fatal(err)
	}
	current["version"] = "usd-owner-integration"
	current["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)
	packs := current["packs"].([]any)
	pack := packs[0].(map[string]any)
	pack["amount_minor"] = float64(pack["amount_minor"].(float64) + 100)
	config, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"config": json.RawMessage(config), "reason": "owner price review"})
	status, _ := admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts", payload)
	if status != http.StatusCreated {
		t.Fatalf("CLI draft status=%d", status)
	}
	for _, op := range []string{"submit", "approve"} {
		body, _ := json.Marshal(map[string]string{"reason": "owner adverse notice met"})
		var response []byte
		status, response = admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts/usd-owner-integration/"+op, body)
		if status != http.StatusOK {
			t.Fatalf("sole-owner %s status=%d body=%s", op, status, response)
		}
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO pricing_audit(actor_id,action,version,change_class,reason,request_id) VALUES(repeat('a',64),'draft_created','synthetic-version','non_adverse','restore fixture','request-fixture')`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"UPDATE pricing_audit SET reason='rewritten'",
		"DELETE FROM pricing_audit",
		"TRUNCATE pricing_audit",
	} {
		if _, err = db.Pool.Exec(ctx, query); err == nil {
			t.Fatalf("audit mutation allowed: %s", query)
		}
	}
	var auditRows int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_audit WHERE actor_id=repeat('a',64) AND reason='restore fixture'").Scan(&auditRows); err != nil || auditRows != 1 {
		t.Fatalf("audit row lost: count=%d err=%v", auditRows, err)
	}
}
