//go:build integration

package pricing_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestActivateDesignChatPricesIntegration(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("pricing_int_test"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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

	ownerID := "00000000-0000-4000-8000-000000000001"
	adminID := "00000000-0000-4000-8000-000000000002"
	for _, id := range []string{ownerID, adminID} {
		if _, err := db.Pool.Exec(ctx, "INSERT INTO learners(id, display_name) VALUES($1::uuid, 'admin') ON CONFLICT DO NOTHING", id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid) ON CONFLICT DO NOTHING", id); err != nil {
			t.Fatal(err)
		}
	}

	admin := pricing.NewAdmin(db.Pool, nil, ownerID)
	admin.SetVerifier(&pricing.DevelopmentVerifier{})

	var baseConfig map[string]any
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-quotes-2026-11-09'").Scan(&baseConfig); err != nil {
		t.Fatal(err)
	}

	makeConfig := func(version string, decision string, effectiveFrom time.Time) map[string]any {
		cfg := make(map[string]any)
		for k, v := range baseConfig {
			cfg[k] = v
		}
		cfg["version"] = version
		if effectiveFrom.IsZero() {
			effectiveFrom = time.Now().UTC().Add(1 * time.Hour)
		}
		cfg["effective_from"] = effectiveFrom.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
		if decision != "" {
			cfg["design_chat_decision"] = decision
		}
		actions := make(map[string]any)
		if a, ok := cfg["actions"].(map[string]any); ok {
			for k, v := range a {
				actions[k] = v
			}
		}
		actions["setup_chat"] = map[string]any{"credits": 0}
		actions["course_brief_chat"] = map[string]any{"credits": 0}
		actions["outline_chat_change"] = map[string]any{"credits": 5}
		cfg["actions"] = actions

		dailyFree := make(map[string]any)
		if df, ok := cfg["daily_free"].(map[string]any); ok {
			for k, v := range df {
				dailyFree[k] = v
			}
		}
		dailyFree["eligible_actions"] = []any{"quick_note", "tutor_short", "tutor_long", "outline_chat_change"}
		cfg["daily_free"] = dailyFree

		limits := make(map[string]any)
		if lim, ok := cfg["limits"].(map[string]any); ok {
			for k, v := range lim {
				limits[k] = v
			}
		}
		limits["setup_chat"] = map[string]any{"input_tokens": 2000, "output_tokens": 1000}
		limits["course_brief_chat"] = map[string]any{
			"input_tokens": 2000, "output_tokens": 1000, "attempt_timeout_ms": 3000,
			"max_attempts": 3, "max_cost_nanos": 2100000, "input_nanos_per_token": 100,
			"output_nanos_per_token": 500, "requests_per_hour": 30, "platform_requests_per_day": 10000,
		}
		limits["outline_chat_change"] = map[string]any{
			"input_tokens": 12000, "output_tokens": 2000, "timeout_ms": 10000,
			"max_attempts": 2, "max_cost_nanos": 5000000,
		}
		cfg["limits"] = limits
		return cfg
	}

	callAdmin := func(actor, method, path string, payload any) (int, string) {
		var body []byte
		if payload != nil {
			body, _ = json.Marshal(payload)
		}
		code, out := admin.ExecuteCLI(ctx, actor, method, path, body)
		return code, string(out)
	}

	t.Run("Production vs Development decision gate isolation", func(t *testing.T) {
		admin.SetEnvironment("production")
		prodCfg := makeConfig("usd-gate-test-prod", "interim_adr_0012", time.Now().UTC().Add(2*time.Hour))
		code, body := callAdmin(adminID, http.MethodPost, "/admin/v1/pricing/drafts", map[string]any{
			"config": prodCfg,
			"reason": "drafting interim in production",
		})
		if code != http.StatusCreated {
			t.Fatalf("draft in prod: code=%d body=%s", code, body)
		}

		code, body = callAdmin(adminID, http.MethodPost, "/admin/v1/pricing/drafts/usd-gate-test-prod/submit", map[string]any{"reason": "submitted"})
		if code != http.StatusOK {
			t.Fatalf("submit in prod: code=%d body=%s", code, body)
		}

		// Non-owner approval must fail in production
		code, body = callAdmin(adminID, http.MethodPost, "/admin/v1/pricing/drafts/usd-gate-test-prod/approve", map[string]any{"reason": "approve"})
		if code != http.StatusForbidden && code != http.StatusUnprocessableEntity {
			t.Fatalf("expected rejection for non-owner in prod, got %d %s", code, body)
		}

		// Owner approval of interim must ALSO fail in production
		code, body = callAdmin(ownerID, http.MethodPost, "/admin/v1/pricing/drafts/usd-gate-test-prod/approve", map[string]any{"reason": "owner approve interim"})
		if code != http.StatusForbidden && code != http.StatusUnprocessableEntity {
			t.Fatalf("expected rejection for interim in prod, got %d %s", code, body)
		}

		// In development: interim decision succeeds
		admin.SetEnvironment("development")
		devCfg := makeConfig("usd-gate-test-dev", "interim_adr_0012", time.Now().UTC().Add(2*time.Hour))
		code, body = callAdmin(adminID, http.MethodPost, "/admin/v1/pricing/drafts", map[string]any{
			"config": devCfg,
			"reason": "drafting interim in dev",
		})
		if code != http.StatusCreated {
			t.Fatalf("draft in dev: code=%d body=%s", code, body)
		}
		code, body = callAdmin(adminID, http.MethodPost, "/admin/v1/pricing/drafts/usd-gate-test-dev/submit", map[string]any{"reason": "submitted"})
		if code != http.StatusOK {
			t.Fatalf("submit in dev: code=%d body=%s", code, body)
		}
		code, body = callAdmin(adminID, http.MethodPost, "/admin/v1/pricing/drafts/usd-gate-test-dev/approve", map[string]any{"reason": "approved in dev"})
		if code != http.StatusOK {
			t.Fatalf("approve in dev: code=%d body=%s", code, body)
		}
	})

	t.Run("Quote version settlement across activation", func(t *testing.T) {
		admin.SetEnvironment("development")
		learnerID := "a0000000-0000-4000-8000-000000000001"
		if _, err := db.Pool.Exec(ctx, "INSERT INTO learners(id, display_name) VALUES($1::uuid, 'learner') ON CONFLICT DO NOTHING", learnerID); err != nil {
			t.Fatal(err)
		}

		initialCat, err := pricing.Load(ctx, db.Queries)
		if err != nil {
			t.Fatal(err)
		}
		financial := credits.NewServiceWithCatalogue(db.Pool, nil, initialCat, nil)

		quotes := pricing.NewQuotes(db.Pool)
		quotes.SetEnvironment("development")

		// Fund learner with 100 credits
		if _, err := db.Pool.Exec(ctx, "INSERT INTO credit_entries(learner_id, amount, kind, reference, pricing_version) VALUES($1::uuid, 100, 'purchase', 'fund-preview', 'usd-quotes-2026-11-09')", learnerID); err != nil {
			t.Fatal(err)
		}

		// Create quote before activation (under usd-quotes-2026-11-09)
		oldQuote, err := quotes.Create(ctx, learnerID, pricing.QuoteInput{Action: "preview"})
		if err != nil {
			t.Fatalf("Create old quote: %v", err)
		}
		if oldQuote.PricingVersion != "usd-quotes-2026-11-09" {
			t.Fatalf("old quote version = %s, want usd-quotes-2026-11-09", oldQuote.PricingVersion)
		}
		if err := quotes.Accept(ctx, learnerID, oldQuote.ID); err != nil {
			t.Fatalf("Accept old quote: %v", err)
		}

		// Reserve under old quote
		res, err := financial.ReserveAction(ctx, learnerID, oldQuote.ID, "preview")
		if err != nil {
			t.Fatalf("ReserveAction old quote: %v", err)
		}

		// Now draft, approve, and activate new version
		eff := time.Now().UTC().Add(800 * time.Millisecond)
		newCfg := makeConfig("usd-settle-test", "interim_adr_0012", eff)
		callAdmin(adminID, http.MethodPost, "/admin/v1/pricing/drafts", map[string]any{"config": newCfg, "reason": "draft"})
		callAdmin(adminID, http.MethodPost, "/admin/v1/pricing/drafts/usd-settle-test/submit", map[string]any{"reason": "submit"})
		code, body := callAdmin(adminID, http.MethodPost, "/admin/v1/pricing/drafts/usd-settle-test/approve", map[string]any{"reason": "approve"})
		if code != http.StatusOK {
			t.Fatalf("approve failed: code=%d body=%s", code, body)
		}

		time.Sleep(900 * time.Millisecond)

		runtime := pricing.NewRuntimeCatalogue(initialCat, db.Queries, db.Pool, nil)
		runtime.SetEnvironment("development")
		if err := runtime.ActivateDue(ctx, admin); err != nil {
			t.Fatalf("ActivateDue: %v", err)
		}

		// Old reservation settles against its original version
		settled, err := financial.Settle(ctx, learnerID, res.ID, []*enginev1.Usage{{
			Provider:         oldQuote.Rate.Provider,
			Model:            oldQuote.Rate.Model,
			Component:        "content",
			Attempt:          1,
			Delivered:        true,
			UsageUnavailable: true,
		}})
		if err != nil {
			t.Fatalf("Settle old reservation: %v", err)
		}
		if settled.State != "settled" {
			t.Fatalf("expected state=settled, got %s", settled.State)
		}
		var settleVersion string
		if err := db.Pool.QueryRow(ctx, "SELECT pricing_version FROM credit_entries WHERE learner_id=$1::uuid AND kind='spend' ORDER BY created_at DESC LIMIT 1", learnerID).Scan(&settleVersion); err != nil {
			t.Fatalf("query spend entry: %v", err)
		}
		if settleVersion != "usd-quotes-2026-11-09" {
			t.Fatalf("settled version = %s, want original usd-quotes-2026-11-09", settleVersion)
		}
	})

	t.Run("Backup and restore preserves active version and action flags", func(t *testing.T) {
		initialCat, err := pricing.Load(ctx, db.Queries)
		if err != nil {
			t.Fatal(err)
		}

		// Exercise custom-format dump and restore
		dumpCode, _, err := container.Exec(ctx, []string{"pg_dump", "-U", "fixture", "-d", "pricing_int_test", "-Fc", "-f", "/tmp/pricing_int.dump"})
		if err != nil || dumpCode != 0 {
			t.Fatalf("pg_dump failed: %d %v", dumpCode, err)
		}

		code, _, err := container.Exec(ctx, []string{"createdb", "-U", "fixture", "pricing_int_restored"})
		if err != nil || code != 0 {
			t.Fatalf("createdb: %d %v", code, err)
		}

		code, _, err = container.Exec(ctx, []string{"pg_restore", "-U", "fixture", "-d", "pricing_int_restored", "--exit-on-error", "/tmp/pricing_int.dump"})
		if err != nil || code != 0 {
			t.Fatalf("pg_restore: %d %v", code, err)
		}

		restoredURL := strings.Replace(url, "/pricing_int_test?", "/pricing_int_restored?", 1)
		restoredStore, err := store.New(ctx, restoredURL)
		if err != nil {
			t.Fatal(err)
		}
		defer restoredStore.Close()

		restoredCat, err := pricing.Load(ctx, restoredStore.Queries)
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(initialCat.Config(), restoredCat.Config()) {
			t.Fatalf("restored catalogue differs from initial: got %+v want %+v", restoredCat.Config(), initialCat.Config())
		}
		if initialCat.HasAction("outline_chat_change") != restoredCat.HasAction("outline_chat_change") {
			t.Fatalf("restored action flag differs: initial=%v restored=%v", initialCat.HasAction("outline_chat_change"), restoredCat.HasAction("outline_chat_change"))
		}
	})
	t.Run("Gated design chat version is skipped without blocking other activation", func(t *testing.T) {
		admin.SetEnvironment("production")
		active, err := pricing.Load(ctx, db.Queries)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		otherAt, gatedAt := now.Add(-200*time.Millisecond), now.Add(-100*time.Millisecond)
		// Inserted outside the draft flow: owner_approved but no reviewer on record.
		gated := makeConfig("usd-gate-nodraft", "owner_approved", gatedAt)
		other := map[string]any{}
		for k, v := range baseConfig {
			other[k] = v
		}
		other["version"] = "usd-gate-unrelated"
		other["effective_from"] = otherAt.Format(time.RFC3339Nano)
		for _, c := range []map[string]any{gated, other} {
			raw, _ := json.Marshal(c)
			if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_versions(version,effective_from,approved,config) VALUES($1,$2,true,$3::jsonb)", c["version"], c["effective_from"], raw); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_quote_policies(version,policy) SELECT $1,policy FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09'", c["version"]); err != nil {
				t.Fatal(err)
			}
		}
		for _, owner := range []string{ownerID, ""} {
			ownerAdmin := pricing.NewAdmin(db.Pool, nil, owner)
			ownerAdmin.SetEnvironment("production")
			ownerAdmin.SetVerifier(&pricing.DevelopmentVerifier{})
			runtime := pricing.NewRuntimeCatalogue(active, db.Queries, db.Pool, nil)
			runtime.SetEnvironment("production")
			if err := runtime.ActivateDue(ctx, ownerAdmin); err != nil {
				t.Fatalf("ActivateDue with owner %q must not fail on a gated version: %v", owner, err)
			}
		}
		var gatedActivated, otherActivated bool
		if err := db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pricing_activation_events WHERE version='usd-gate-nodraft'), EXISTS(SELECT 1 FROM pricing_activation_events WHERE version='usd-gate-unrelated')").Scan(&gatedActivated, &otherActivated); err != nil {
			t.Fatal(err)
		}
		if gatedActivated {
			t.Fatal("owner_approved version without owner reviewer was activated in production")
		}
		if !otherActivated {
			t.Fatal("unrelated due version was not activated")
		}

		// Even if such a version somehow became active, production quotes fail closed
		// for an empty PRICING_OWNER_ID and for a missing reviewer.
		if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_activation_events(version,activated_at) VALUES('usd-gate-nodraft',$1)", gatedAt); err != nil {
			t.Fatal(err)
		}
		learnerID := "a0000000-0000-4000-8000-000000000002"
		if _, err := db.Pool.Exec(ctx, "INSERT INTO learners(id, display_name) VALUES($1::uuid, 'learner') ON CONFLICT DO NOTHING", learnerID); err != nil {
			t.Fatal(err)
		}
		dev := pricing.NewQuotes(db.Pool)
		dev.SetEnvironment("development")
		if _, err := dev.Create(ctx, learnerID, pricing.QuoteInput{Action: "setup_chat"}); err != nil {
			t.Fatalf("development control quote: %v", err)
		}
		for _, owner := range []string{"", ownerID} {
			prod := pricing.NewQuotes(db.Pool)
			prod.SetEnvironment("production")
			prod.SetOwnerID(owner)
			if _, err := prod.Create(ctx, learnerID, pricing.QuoteInput{Action: "setup_chat"}); err == nil {
				t.Fatalf("production quote succeeded without owner reviewer (owner=%q)", owner)
			}
		}
	})

	t.Run("LoadDevelopment does not resurrect rolled-back design chat actions", func(t *testing.T) {
		before, err := pricing.LoadDevelopment(ctx, db.Pool)
		if err != nil || !before.HasAction("course_brief_chat") {
			t.Fatalf("expected active design chat version before rollback: %v", err)
		}
		rollback := map[string]any{}
		for k, v := range baseConfig {
			rollback[k] = v
		}
		at := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Millisecond)
		rollback["version"] = "usd-gate-rollback"
		rollback["effective_from"] = at.Format(time.RFC3339Nano)
		raw, _ := json.Marshal(rollback)
		if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_versions(version,effective_from,approved,config) VALUES('usd-gate-rollback',$1,true,$2::jsonb)", at, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_quote_policies(version,policy) SELECT 'usd-gate-rollback',policy FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09'"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_activation_events(version,activated_at) VALUES('usd-gate-rollback',$1)", at); err != nil {
			t.Fatal(err)
		}
		after, err := pricing.LoadDevelopment(ctx, db.Pool)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range []string{"setup_chat", "course_brief_chat", "outline_chat_change"} {
			if after.HasAction(a) {
				t.Fatalf("%s resurrected after rollback (version %s)", a, after.Config().Version)
			}
		}
	})

}
