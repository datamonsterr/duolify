//go:build integration

package pricing_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type pricingActivationCapture struct {
	mu     sync.Mutex
	events []map[string]any
}

func (c *pricingActivationCapture) Capture(id string, event string, properties map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := map[string]any{"distinct_id": id, "event": event, "properties": properties}
	c.events = append(c.events, entry)
	return nil
}
func (*pricingActivationCapture) Close() error { return nil }

func TestScheduleCancelRuntimeFlipAndImmutableRollbackAncestry(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("pricing_runtime"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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

	initial, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	events := &pricingActivationCapture{}
	runtime := pricing.NewRuntimeCatalogue(initial, db.Queries, db.Pool, events)
	activatorCtx, stopActivator := context.WithCancel(ctx)
	defer stopActivator()
	activatorAdmin := pricing.NewAdmin(db.Pool, nil, "")
	activatorAdmin.SetVerifier(&pricing.DevelopmentVerifier{})

	read := func() map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		runtime.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/pricing", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("runtime GET status=%d body=%s", w.Code, w.Body)
		}
		var response map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}

	// The schedule instant and activation boundary come from PostgreSQL, never
	// the test process clock. GET exposes next_scheduled before the boundary.
	var scheduledAt time.Time
	if err = db.Pool.QueryRow(ctx, "SELECT now()+interval '800 milliseconds'").Scan(&scheduledAt); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-launch-2026-11-08'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config["version"] = "usd-runtime-flip"
	config["effective_from"] = scheduledAt.UTC().Format(time.RFC3339Nano)
	config["actions"].(map[string]any)["preview"].(map[string]any)["credits"] = float64(24)
	encoded, _ := json.Marshal(config)
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_versions(version,effective_from,approved,config) VALUES($1,$2,true,$3)", "usd-runtime-flip", scheduledAt, encoded); err != nil {
		t.Fatal(err)
	}
	// Mirror approval's execution-policy inheritance: E7-03 requires every
	// approved successor to retain its parent's operational bounds.
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_quote_policies(version,policy) SELECT 'usd-runtime-flip',policy FROM pricing_quote_policies WHERE version=$1", initial.Config().Version); err != nil {
		t.Fatal(err)
	}
	before := read()
	if before["version"] != "usd-quotes-2026-11-09" || before["next_scheduled"] == nil {
		t.Fatalf("scheduled response=%v", before)
	}

	// Concurrent readers at the database-time flip converge on the same
	// greatest effective_from version. A missing Redis cache is harmless: the
	// resolver reads Postgres as its source of truth.
	if _, err = db.Pool.Exec(ctx, "SELECT pg_sleep(.9)"); err != nil {
		t.Fatal(err)
	}
	// A due successor cannot activate without a verifier, including on GET.
	unverifiedAdmin := pricing.NewAdmin(db.Pool, nil, "")
	if err := runtime.ActivateDue(ctx, unverifiedAdmin); err == nil {
		t.Fatal("activation accepted absent verifier")
	}
	if got := read(); got["version"] != "usd-quotes-2026-11-09" {
		t.Fatalf("unverified successor exposed: %v", got)
	}
	// Run two independent replicas to exercise the shared transaction marker.
	go runtime.RunActivator(activatorCtx, activatorAdmin)
	replica := pricing.NewRuntimeCatalogue(initial, db.Queries, db.Pool, events)
	go replica.RunActivator(activatorCtx, activatorAdmin)
	// Activation must happen without any public GET.
	var beforeReads int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_audit WHERE action='version_activated' AND version='usd-runtime-flip'").Scan(&beforeReads); err != nil {
			t.Fatal(err)
		}
		if beforeReads == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if beforeReads != 1 {
		t.Fatalf("activation required public GET: rows=%d", beforeReads)
	}
	const readers = 12
	var wg sync.WaitGroup
	errCh := make(chan any, readers)
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := read()
			if got["version"] != "usd-runtime-flip" {
				errCh <- got
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for got := range errCh {
		t.Errorf("concurrent resolver returned %v", got)
	}
	events.mu.Lock()
	if len(events.events) != 2 || events.events[0]["event"] != "pricing_version_activated" || events.events[1]["event"] != "price_table_activated" {
		t.Errorf("activation events delivered %v", events.events)
	}
	for _, event := range events.events {
		properties := event["properties"].(map[string]any)
		if event["distinct_id"] != "00000000-0000-4000-8000-000000000011" || properties["actor_id"] != event["distinct_id"] || properties["pricing_version"] != "usd-runtime-flip" {
			t.Errorf("invalid activation common properties: %v", event)
		}
		if _, private := properties["reason"]; private {
			t.Errorf("private reason emitted: %v", event)
		}
	}
	events.mu.Unlock()
	var activationRows int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_activation_events WHERE version='usd-runtime-flip'").Scan(&activationRows); err != nil || activationRows != 1 {
		t.Fatalf("activation marker rows=%d err=%v", activationRows, err)
	}
	var activationTime, auditTime time.Time
	if err = db.Pool.QueryRow(ctx, "SELECT e.activated_at,a.created_at FROM pricing_activation_events e JOIN pricing_audit a ON a.version=e.version AND a.action='version_activated' WHERE e.version='usd-runtime-flip'").Scan(&activationTime, &auditTime); err != nil || !activationTime.Equal(scheduledAt) || !auditTime.Equal(scheduledAt) {
		t.Fatalf("activation=%s audit=%s scheduled=%s err=%v", activationTime, auditTime, scheduledAt, err)
	}
	var activationAudit int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_audit WHERE action='version_activated' AND version='usd-runtime-flip'").Scan(&activationAudit); err != nil || activationAudit != 1 {
		t.Fatalf("activation audit rows=%d err=%v", activationAudit, err)
	}

	// Cancellation is based on database time and leaves immutable version
	// content untouched; the prior version resolves again.
	var actor string
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('pricing runtime actor') RETURNING id::text").Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", actor); err != nil {
		t.Fatal(err)
	}
	var approver string
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('pricing second approver') RETURNING id::text").Scan(&approver); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", approver); err != nil {
		t.Fatal(err)
	}
	admin := pricing.NewAdmin(db.Pool, nil, "")
	admin.SetVerifier(&pricing.DevelopmentVerifier{})
	var future time.Time
	if err = db.Pool.QueryRow(ctx, "SELECT now()+interval '2 hours'").Scan(&future); err != nil {
		t.Fatal(err)
	}
	var futureConfig map[string]any
	_ = json.Unmarshal(raw, &futureConfig)
	futureConfig["version"] = "usd-cancelled"
	futureConfig["effective_from"] = future.UTC().Format(time.RFC3339Nano)
	futureJSON, _ := json.Marshal(futureConfig)
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_versions(version,effective_from,approved,config) VALUES($1,$2,true,$3)", "usd-cancelled", future, futureJSON); err != nil {
		t.Fatal(err)
	}
	status, _ := admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts/usd-cancelled/cancel", []byte(`{"reason":"reschedule approved catalogue"}`))
	if status != http.StatusOK {
		t.Fatalf("cancel status=%d", status)
	}
	if got := read(); got["next_scheduled"] != nil {
		t.Fatalf("cancelled schedule remained visible: %v", got["next_scheduled"])
	}
	for _, path := range []string{"/admin/v1/pricing/drafts/usd-cancelled?reason=review", "/admin/v1/pricing/drafts?reason=review"} {
		status, body := admin.ExecuteCLI(ctx, actor, http.MethodGet, path, nil)
		if status != 200 || !strings.Contains(string(body), `"status":"cancelled"`) {
			t.Fatalf("cancelled status=%d body=%s", status, body)
		}
	}
	var unchanged string
	if err = db.Pool.QueryRow(ctx, "SELECT config::text FROM pricing_versions WHERE version='usd-cancelled'").Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(unchanged)) {
		t.Fatalf("immutable version was damaged: %s", unchanged)
	}

	// Rollback creates a reviewed child draft whose learner-facing content is
	// copied from the historical source and whose parent is the current active.
	var rollbackAt time.Time
	if err = db.Pool.QueryRow(ctx, "SELECT now()+interval '96 hours'").Scan(&rollbackAt); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"version": "usd-runtime-rollback", "effective_from": rollbackAt, "reason": "restore prior approved price table"})
	status, response := admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts/usd-launch-2026-11-08/rollback", body)
	if status != http.StatusCreated {
		t.Fatalf("rollback status=%d body=%s", status, response)
	}
	var draft struct {
		Version string `json:"version"`
		Parent  string `json:"parent_version"`
		Status  string `json:"status"`
	}
	if err = json.Unmarshal(response, &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Version != "usd-runtime-rollback" || draft.Parent != "usd-runtime-flip" || draft.Status != "draft" {
		t.Fatalf("rollback ancestry=%+v", draft)
	}
	var sourceVersion string
	if err = db.Pool.QueryRow(ctx, "SELECT rollback_source_version FROM pricing_drafts WHERE version=$1", draft.Version).Scan(&sourceVersion); err != nil || sourceVersion != "usd-launch-2026-11-08" {
		t.Fatalf("rollback source=%q err=%v", sourceVersion, err)
	}
	var rollbackClass string
	if err = db.Pool.QueryRow(ctx, "SELECT change_class FROM pricing_drafts WHERE version=$1", draft.Version).Scan(&rollbackClass); err != nil || rollbackClass != "adverse" {
		t.Fatalf("higher-price rollback class=%q err=%v", rollbackClass, err)
	}
	status, response = admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts/"+draft.Version+"/submit", []byte(`{"reason":"submit higher-price rollback for review"}`))
	if status != http.StatusOK {
		t.Fatalf("rollback submit status=%d body=%s", status, response)
	}
	status, response = admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts/"+draft.Version+"/approve", []byte(`{"reason":"separation of duties"}`))
	if status != http.StatusForbidden {
		t.Fatalf("rollback self-approval status=%d body=%s", status, response)
	}
	status, response = admin.ExecuteCLI(ctx, approver, http.MethodPost, "/admin/v1/pricing/drafts/"+draft.Version+"/approve", []byte(`{"reason":"approved higher-price rollback after notice"}`))
	if status != http.StatusOK {
		t.Fatalf("second-admin rollback approval status=%d body=%s", status, response)
	}
	var rollbackConfig, sourceConfig []byte
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_drafts WHERE version=$1", draft.Version).Scan(&rollbackConfig); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-launch-2026-11-08'").Scan(&sourceConfig); err != nil {
		t.Fatal(err)
	}
	var restored, source map[string]any
	_ = json.Unmarshal(rollbackConfig, &restored)
	_ = json.Unmarshal(sourceConfig, &source)
	delete(restored, "version")
	delete(source, "version")
	delete(restored, "effective_from")
	delete(source, "effective_from")
	rb, _ := json.Marshal(restored)
	sb, _ := json.Marshal(source)
	if string(rb) != string(sb) {
		t.Fatalf("rollback changed historical terms\n got=%s\nwant=%s", rb, sb)
	}
}
