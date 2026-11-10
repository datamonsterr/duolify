//go:build integration

package pricing_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestE12ReviewGuards(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("pricing_review"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('review owner') RETURNING id::text").Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", actor); err != nil {
		t.Fatal(err)
	}
	admin := pricing.NewAdmin(db.Pool, nil, actor)
	draft := func(t *testing.T, version string, effective time.Time, change func(map[string]any)) {
		t.Helper()
		var config map[string]any
		if err := db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-quotes-2026-11-09'").Scan(&config); err != nil {
			t.Fatal(err)
		}
		config["version"] = version
		config["effective_from"] = effective.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
		if change != nil {
			change(config)
		}
		raw, _ := json.Marshal(config)
		if _, err := db.Pool.Exec(ctx, `INSERT INTO pricing_drafts(version,parent_version,config,created_by,change_class,status,submitted_at) VALUES($1,'usd-quotes-2026-11-09',$2,$3,'non_adverse','submitted',now())`, version, raw, actor); err != nil {
			t.Fatal(err)
		}
	}
	approve := func(version string) (int, []byte) {
		return admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts/"+version+"/approve", []byte(`{"reason":"review guards"}`))
	}
	t.Run("nil verifier approval fails closed", func(t *testing.T) {
		draft(t, "usd-review-no-verifier", time.Now().Add(96*time.Hour), nil)
		status, body := approve("usd-review-no-verifier")
		if status != 422 || !strings.Contains(string(body), "verifier") {
			t.Fatalf("status=%d body=%s", status, body)
		}
		var count int
		if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_versions WHERE version='usd-review-no-verifier'").Scan(&count); err != nil || count != 0 {
			t.Fatalf("persisted approval=%d err=%v", count, err)
		}
	})
	admin.SetVerifier(&pricing.DevelopmentVerifier{})
	t.Run("past effective instant", func(t *testing.T) {
		draft(t, "usd-review-past", time.Now().Add(-time.Hour), nil)
		status, body := approve("usd-review-past")
		if status != 409 || !strings.Contains(string(body), "effective") {
			t.Fatalf("status=%d body=%s", status, body)
		}
	})
	t.Run("rollback before active version", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"version": "usd-review-old-rollback", "effective_from": time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), "reason": "review rollback"})
		status, response := admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts/usd-launch-2026-11-08/rollback", body)
		if status != 409 || !strings.Contains(string(response), "effective") {
			t.Fatalf("status=%d body=%s", status, response)
		}
	})
	t.Run("evidence failure rolls approval back", func(t *testing.T) {
		draft(t, "usd-review-evidence-failure", time.Now().Add(96*time.Hour), nil)
		if _, err := db.Pool.Exec(ctx, `CREATE FUNCTION reject_review_evidence() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'evidence unavailable'; END $$; CREATE TRIGGER reject_review_evidence BEFORE INSERT ON pricing_verification_evidence FOR EACH ROW EXECUTE FUNCTION reject_review_evidence()`); err != nil {
			t.Fatal(err)
		}
		status, body := approve("usd-review-evidence-failure")
		if status != 409 {
			t.Fatalf("status=%d body=%s", status, body)
		}
		var count int
		if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_versions WHERE version='usd-review-evidence-failure'").Scan(&count); err != nil || count != 0 {
			t.Fatalf("persisted versions=%d err=%v", count, err)
		}
		if _, err := db.Pool.Exec(ctx, "DROP TRIGGER reject_review_evidence ON pricing_verification_evidence"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("override requires second administrator and is audited", func(t *testing.T) {
		draft(t, "usd-review-override", time.Now().Add(96*time.Hour), func(cfg map[string]any) {
			cfg["actions"].(map[string]any)["lesson_content"].(map[string]any)["credits"] = 60
		})
		reason := []byte(`{"reason":"review promotion","override_reason":"bounded promotion reviewed"}`)
		status, body := admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts/usd-review-override/approve", reason)
		if status != 403 {
			t.Fatalf("sole owner override status=%d body=%s", status, body)
		}
		var reviewer string
		if err := db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('second reviewer') RETURNING id::text").Scan(&reviewer); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", reviewer); err != nil {
			t.Fatal(err)
		}
		status, body = admin.ExecuteCLI(ctx, reviewer, http.MethodPost, "/admin/v1/pricing/drafts/usd-review-override/approve", reason)
		if status != 200 {
			t.Fatalf("second reviewer status=%d body=%s", status, body)
		}
		var override string
		if err := db.Pool.QueryRow(ctx, "SELECT override_reason FROM pricing_audit WHERE version='usd-review-override' AND action='version_approved'").Scan(&override); err != nil || override != "bounded promotion reviewed" {
			t.Fatalf("override=%q err=%v", override, err)
		}
		var evidence map[string]any
		if err := db.Pool.QueryRow(ctx, "SELECT evidence FROM pricing_verification_evidence WHERE version='usd-review-override'").Scan(&evidence); err != nil {
			t.Fatal(err)
		}
		checks, _ := evidence["checks"].([]any)
		if len(checks) < 9 || evidence["mappings_verified"] != true {
			t.Fatalf("missing actual verification checks: %v", evidence)
		}
		if _, fabricated := evidence["models"]; fabricated {
			t.Fatal("unverified model evidence was stored")
		}
	})
	t.Run("restart reconciles missed activation and requires verifier", func(t *testing.T) {
		effective := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
		draft(t, "usd-review-missed-activation", effective, nil)
		if _, err := db.Pool.Exec(ctx, `INSERT INTO pricing_versions(version,effective_from,approved,config) SELECT version,$1,true,config FROM pricing_drafts WHERE version='usd-review-missed-activation'`, effective); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, "UPDATE pricing_drafts SET status='approved' WHERE version='usd-review-missed-activation'"); err != nil {
			t.Fatal(err)
		}
		initial, err := pricing.Load(ctx, db.Queries)
		if err != nil {
			t.Fatal(err)
		}
		runtime := pricing.NewRuntimeCatalogue(initial, db.Queries, db.Pool, nil)
		absent := pricing.NewAdmin(db.Pool, nil, "")
		if err := runtime.ActivateDue(ctx, absent); err == nil {
			t.Fatal("restart activated with absent verifier")
		}
		if err := runtime.ActivateDue(ctx, admin); err != nil {
			t.Fatal(err)
		}
		if err := runtime.ActivateDue(ctx, admin); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_audit WHERE version='usd-review-missed-activation' AND action='version_activated'").Scan(&count); err != nil || count != 1 {
			t.Fatalf("restart activation audit count=%d err=%v", count, err)
		}
	})
	t.Run("stale package session requires reauthentication", func(t *testing.T) {
		mail := new(accounts.MailSink)
		auth, err := accounts.New(db.Pool, "test-only-signing-key-at-least-32-bytes", mail)
		if err != nil {
			t.Fatal(err)
		}
		call := func(path, body string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			auth.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(body)))
			return w
		}
		if w := call("/v1/auth/code", `{"email":"review@example.test","age_confirmed":true}`); w.Code != 202 {
			t.Fatal(w.Body)
		}
		w := call("/v1/auth/verify", `{"email":"review@example.test","age_confirmed":true,"code":"`+mail.Code("review@example.test")+`"}`)
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
		var tokens accounts.Tokens
		if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
			t.Fatal(err)
		}
		claims, err := auth.ParseAccess(tokens.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", claims.Subject); err != nil {
			t.Fatal(err)
		}
		if ok, err := pricing.HasAdminAccess(ctx, db.Pool, claims.Subject, claims.ID); err != nil || !ok {
			t.Fatalf("fresh allowed=%v err=%v", ok, err)
		}
		if _, err = db.Pool.Exec(ctx, "UPDATE auth_sessions SET reauthenticated_at=now()-interval '16 minutes' WHERE id=$1::uuid", claims.ID); err != nil {
			t.Fatal(err)
		}
		if ok, err := pricing.HasAdminAccess(ctx, db.Pool, claims.Subject, claims.ID); err != nil || ok {
			t.Fatalf("stale allowed=%v err=%v", ok, err)
		}
	})
}
