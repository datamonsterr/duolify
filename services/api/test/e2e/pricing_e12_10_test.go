package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPricingE1210ActivateAndRollbackThroughHTTP(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("pricing_e12_10"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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

	var captureMu sync.Mutex
	var captured []map[string]any
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Error(err)
		}
		captureMu.Lock()
		captured = append(captured, batch.Batch...)
		captureMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()

	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, "test-only-signing-key-at-least-32-bytes", mail)
	if err != nil {
		t.Fatal(err)
	}

	login := func(email string) (string, string) {
		t.Helper()
		call := func(path, body string) *httptest.ResponseRecorder {
			r := httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(body))
			w := httptest.NewRecorder()
			auth.ServeHTTP(w, r)
			return w
		}
		if w := call("/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`); w.Code != 202 {
			t.Fatal(w.Body)
		}
		w := call("/v1/auth/verify", `{"email":"`+email+`","age_confirmed":true,"code":"`+mail.Code(email)+`"}`)
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
		return tokens.AccessToken, claims.Subject
	}

	authorToken, authorID := login("pricing-author@example.test")
	approverToken, approverID := login("pricing-approver@example.test")
	ownerToken, ownerID := login("pricing-owner@example.test")

	for _, id := range []string{authorID, approverID, ownerID} {
		if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid) ON CONFLICT DO NOTHING", id); err != nil {
			t.Fatal(err)
		}
	}

	events, err := analytics.New("synthetic-project-key", sink.URL, func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}

	admin := pricing.NewAdmin(db.Pool, events, ownerID)
	admin.SetVerifier(&pricing.DevelopmentVerifier{})

	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}

	quotesService := pricing.NewQuotes(db.Pool)
	runtimePricing := pricing.NewRuntimeCatalogue(catalogue, db.Queries, db.Pool, events)

	handler := httpapi.New(httpapi.Dependencies{
		Accounts:     auth,
		Pricing:      runtimePricing,
		PricingAdmin: admin,
		Quotes:       quotesService,
		Postgres:     db.Ping,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	server := httptest.NewServer(handler)
	defer server.Close()

	request := func(method, path, token string, body []byte) *httptest.ResponseRecorder {
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		bodyBytes, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		w.Code = response.StatusCode
		_, _ = w.Body.Write(bodyBytes)
		return w
	}

	// 1. Prepare base interim pricing config
	var baseConfig map[string]any
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-quotes-2026-11-09'").Scan(&baseConfig); err != nil {
		t.Fatal(err)
	}

	makeInterimConfig := func(version string, decision string, effectiveFrom time.Time) map[string]any {
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
		eligible := []any{"quick_note", "tutor_short", "tutor_long", "outline_chat_change"}
		dailyFree["eligible_actions"] = eligible
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

	t.Run("Production mode rejects unapproved design chat pricing", func(t *testing.T) {
		// Set admin to production mode
		admin.SetEnvironment("production")
		defer admin.SetEnvironment("development")

		prodConfig := makeInterimConfig("usd-prod-rejected-1", "interim_adr_0012", time.Now().UTC().Add(2*time.Hour))
		payload, _ := json.Marshal(map[string]any{
			"config": prodConfig,
			"reason": "attempt to draft interim design chat in production",
		})
		w := request(http.MethodPost, "/admin/v1/pricing/drafts", authorToken, payload)
		if w.Code != http.StatusCreated {
			t.Fatalf("draft creation failed: status=%d body=%s", w.Code, w.Body)
		}

		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-prod-rejected-1/submit", authorToken, []byte(`{"reason":"ready for review"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("submit failed: status=%d body=%s", w.Code, w.Body)
		}

		// Non-owner approval must be rejected in production
		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-prod-rejected-1/approve", approverToken, []byte(`{"reason":"approving interim in prod"}`))
		if w.Code != http.StatusForbidden && w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 403 or 422 for interim approval in production, got %d body=%s", w.Code, w.Body)
		}

		// Owner approval of interim_adr_0012 must ALSO be rejected in production (requires owner_approved)
		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-prod-rejected-1/approve", ownerToken, []byte(`{"reason":"owner approving interim in prod"}`))
		if w.Code != http.StatusForbidden && w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 403 or 422 for interim decision approval in production, got %d body=%s", w.Code, w.Body)
		}
	})

	t.Run("Dev/test mode activates interim pricing and enables actions", func(t *testing.T) {
		admin.SetEnvironment("development")
		runtimePricing.SetEnvironment("development")
		quotesService.SetEnvironment("development")

		effective := time.Now().UTC().Add(800 * time.Millisecond)
		interimConfig := makeInterimConfig("usd-interim-e12-10", "interim_adr_0012", effective)
		payload, _ := json.Marshal(map[string]any{
			"config": interimConfig,
			"reason": "interim ADR 0012 design chat pricing",
		})

		w := request(http.MethodPost, "/admin/v1/pricing/drafts", authorToken, payload)
		if w.Code != http.StatusCreated {
			t.Fatalf("draft creation failed: status=%d body=%s", w.Code, w.Body)
		}

		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-interim-e12-10/submit", authorToken, []byte(`{"reason":"ready for dev review"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("submit failed: status=%d body=%s", w.Code, w.Body)
		}

		// In development, approval of interim_adr_0012 succeeds
		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-interim-e12-10/approve", approverToken, []byte(`{"reason":"approved interim for dev/test"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("approval failed: status=%d body=%s", w.Code, w.Body)
		}

		// Wait until effective_from has passed in DB
		time.Sleep(900 * time.Millisecond)

		// Activate the due version
		if err := runtimePricing.ActivateDue(ctx, admin); err != nil {
			t.Fatalf("ActivateDue failed: %v", err)
		}

		// GET /v1/pricing should now reflect the activated interim version
		w = request(http.MethodGet, "/v1/pricing", "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /v1/pricing failed: status=%d body=%s", w.Code, w.Body)
		}
		var publicPricing map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &publicPricing); err != nil {
			t.Fatal(err)
		}
		if publicPricing["version"] != "usd-interim-e12-10" {
			t.Fatalf("active version = %v, want usd-interim-e12-10", publicPricing["version"])
		}

		actions, _ := publicPricing["actions"].(map[string]any)
		if oc, ok := actions["outline_chat_change"].(map[string]any); !ok || int(oc["credits"].(float64)) != 5 {
			t.Fatalf("expected outline_chat_change=5, got %+v", actions["outline_chat_change"])
		}
		if sc, ok := actions["setup_chat"].(map[string]any); !ok || int(sc["credits"].(float64)) != 0 {
			t.Fatalf("expected setup_chat=0, got %+v", actions["setup_chat"])
		}
		if cbc, ok := actions["course_brief_chat"].(map[string]any); !ok || int(cbc["credits"].(float64)) != 0 {
			t.Fatalf("expected course_brief_chat=0, got %+v", actions["course_brief_chat"])
		}

		// Action quote: POST /v1/quotes for setup_chat should succeed
		w = request(http.MethodPost, "/v1/quotes", authorToken, []byte(`{"action":"setup_chat"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("setup_chat quote failed: status=%d body=%s", w.Code, w.Body)
		}
		var setupQuote map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &setupQuote); err != nil {
			t.Fatal(err)
		}
		if setupQuote["pricing_version"] != "usd-interim-e12-10" || int(setupQuote["content_ceiling"].(float64)) != 0 {
			t.Fatalf("unexpected setup_chat quote: %+v", setupQuote)
		}

		// Create draft and brief revision to quote outline_chat_change
		var briefID string
		briefHash := fmt.Sprintf("%064x", sha256.Sum256([]byte("e2e-brief")))
		err = db.Pool.QueryRow(ctx, "INSERT INTO course_brief_revisions(learner_id, brief, content_hash) VALUES($1::uuid, '{\"topic\":\"Go\"}'::jsonb, $2) RETURNING id::text", authorID, briefHash).Scan(&briefID)
		if err != nil {
			t.Fatal(err)
		}
		var draftID string
		err = db.Pool.QueryRow(ctx, "INSERT INTO outline_drafts(learner_id, brief_revision, revision, outline) VALUES($1::uuid, $2::uuid, 1, '[{\"id\":\"u1\",\"title\":\"Intro\",\"lessons\":[{\"id\":\"l1\",\"title\":\"L1\",\"goal\":\"G1\"}]}]'::jsonb) RETURNING id::text", authorID, briefID).Scan(&draftID)
		if err != nil {
			t.Fatal(err)
		}
		reqHash := hex.EncodeToString(bytes.Repeat([]byte{0xaa}, 32))

		// Quote outline_chat_change under the activated version
		quotePayload, _ := json.Marshal(map[string]any{
			"action":           "outline_chat_change",
			"outline_draft_id": draftID,
			"outline_revision": 1,
			"request_hash":     reqHash,
		})
		w = request(http.MethodPost, "/v1/quotes", authorToken, quotePayload)
		if w.Code != http.StatusOK {
			t.Fatalf("outline_chat_change quote failed: status=%d body=%s", w.Code, w.Body)
		}
		var outlineQuote map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &outlineQuote); err != nil {
			t.Fatal(err)
		}
		if outlineQuote["pricing_version"] != "usd-interim-e12-10" {
			t.Fatalf("outline_chat_change quote pricing_version = %v, want usd-interim-e12-10", outlineQuote["pricing_version"])
		}
		if int(outlineQuote["content_ceiling"].(float64)) != 5 {
			t.Fatalf("outline_chat_change content_ceiling = %v, want 5", outlineQuote["content_ceiling"])
		}
		if !outlineQuote["daily_free_eligible"].(bool) {
			t.Fatalf("outline_chat_change should be daily free eligible")
		}
	})

	t.Run("Forward rollback disables design chat actions while preserving catalogue", func(t *testing.T) {
		// Roll back to usd-quotes-2026-11-09 (which lacks design chat actions)
		rollbackEffective := time.Now().UTC().Add(800 * time.Millisecond)
		rollbackPayload, _ := json.Marshal(map[string]any{
			"version":        "usd-rollback-e12-10",
			"effective_from": rollbackEffective.Format(time.RFC3339Nano),
			"reason":         "reverting design chat prices to base catalogue",
		})
		w := request(http.MethodPost, "/admin/v1/pricing/drafts/usd-quotes-2026-11-09/rollback", authorToken, rollbackPayload)
		if w.Code != http.StatusCreated {
			t.Fatalf("rollback draft failed: status=%d body=%s", w.Code, w.Body)
		}

		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-rollback-e12-10/submit", authorToken, []byte(`{"reason":"ready to rollback"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("rollback submit failed: status=%d body=%s", w.Code, w.Body)
		}

		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-rollback-e12-10/approve", approverToken, []byte(`{"reason":"approve rollback"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("rollback approve failed: status=%d body=%s", w.Code, w.Body)
		}

		// Wait until rollback effective_from has passed in DB
		time.Sleep(900 * time.Millisecond)

		if err := runtimePricing.ActivateDue(ctx, admin); err != nil {
			t.Fatalf("ActivateDue after rollback failed: %v", err)
		}

		// GET /v1/pricing should now reflect the rollback version
		w = request(http.MethodGet, "/v1/pricing", "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /v1/pricing failed: status=%d body=%s", w.Code, w.Body)
		}
		var publicPricing map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &publicPricing); err != nil {
			t.Fatal(err)
		}
		if publicPricing["version"] != "usd-rollback-e12-10" {
			t.Fatalf("active version = %v, want usd-rollback-e12-10", publicPricing["version"])
		}

		actions, _ := publicPricing["actions"].(map[string]any)
		if _, ok := actions["outline_chat_change"]; ok {
			t.Fatalf("outline_chat_change should not be present after rollback")
		}
		if _, ok := actions["setup_chat"]; ok {
			t.Fatalf("setup_chat should not be present after rollback")
		}
		if _, ok := actions["course_brief_chat"]; ok {
			t.Fatalf("course_brief_chat should not be present after rollback")
		}

		// Unrelated catalogue actions MUST remain active and unchanged
		if oc, ok := actions["outline"].(map[string]any); !ok || int(oc["credits"].(float64)) != 55 {
			t.Fatalf("outline action should remain active with 55 credits, got %+v", actions["outline"])
		}
		if pc, ok := actions["preview"].(map[string]any); !ok || int(pc["credits"].(float64)) != 25 {
			t.Fatalf("preview action should remain active with 25 credits, got %+v", actions["preview"])
		}

		// Quoting setup_chat now must fail (disabled after rollback)
		w = request(http.MethodPost, "/v1/quotes", authorToken, []byte(`{"action":"setup_chat"}`))
		if w.Code != http.StatusConflict && w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected quote failure for disabled setup_chat, got %d body=%s", w.Code, w.Body)
		}

		// Quoting unrelated action preview must still succeed
		w = request(http.MethodPost, "/v1/quotes", authorToken, []byte(`{"action":"preview"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("preview quote should still succeed after rollback: status=%d body=%s", w.Code, w.Body)
		}
	})

	t.Run("Analytics events emitted without reason or chat text", func(t *testing.T) {
		if err = events.Close(); err != nil {
			t.Fatal(err)
		}

		captureMu.Lock()
		defer captureMu.Unlock()

		seen := map[string]bool{}
		for _, event := range captured {
			name, _ := event["event"].(string)
			seen[name] = true
			props, _ := event["properties"].(map[string]any)

			// Common properties required
			if props["source"] != "api" || props["platform"] != "web" {
				t.Fatalf("event %s missing common properties: %+v", name, props)
			}
			// Private text strictly forbidden
			if _, hasReason := props["reason"]; hasReason {
				t.Fatalf("event %s leaked reason: %+v", name, props)
			}
			raw, _ := json.Marshal(event)
			if strings.Contains(string(raw), "interim ADR 0012") || strings.Contains(string(raw), "reverting design chat") {
				t.Fatalf("event leaked operator reason in payload: %s", raw)
			}
		}

		if !seen["pricing_version_approved"] {
			t.Fatalf("missing pricing_version_approved in captured events: %+v", captured)
		}
		if !seen["pricing_version_activated"] {
			t.Fatalf("missing pricing_version_activated in captured events: %+v", captured)
		}
		if !seen["pricing_version_rolled_back"] {
			t.Fatalf("missing pricing_version_rolled_back in captured events: %+v", captured)
		}
	})
}
