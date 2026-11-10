package e2e

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestPricingDraftAndApproveThroughHTTP(t *testing.T) {
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
	author, authorID := login("pricing-author@example.test")
	approver, approverID := login("pricing-approver@example.test")
	for _, id := range []string{authorID, approverID} {
		if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", id); err != nil {
			t.Fatal(err)
		}
	}
	events, err := analytics.New("synthetic-project-key", sink.URL, func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	admin := pricing.NewAdmin(db.Pool, events, "")
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(httpapi.Dependencies{Accounts: auth, Pricing: catalogue, PricingAdmin: admin, Quotes: pricing.NewQuotes(db.Pool), Postgres: db.Ping}, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	unauthorized := request(http.MethodGet, "/admin/v1/pricing/drafts?reason=unauthorized", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous pricing admin status=%d", unauthorized.Code)
	}
	var current map[string]any
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-launch-2026-11-08'").Scan(&current); err != nil {
		t.Fatal(err)
	}
	current["version"] = "usd-admin-e12-05-1"
	current["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)
	packs := current["packs"].([]any)
	pack := packs[0].(map[string]any)
	pack["amount_minor"] = float64(pack["amount_minor"].(float64) + 100)
	config, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"config": json.RawMessage(config), "reason": "approved product cost update"})
	w := request(http.MethodPost, "/admin/v1/pricing/drafts", author, payload)
	if w.Code != http.StatusCreated {
		t.Fatalf("draft status=%d body=%s", w.Code, w.Body)
	}
	w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-admin-e12-05-1/submit", author, []byte(`{"reason":"ready for owner review"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("submit status=%d body=%s", w.Code, w.Body)
	}
	w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-admin-e12-05-1/approve", author, []byte(`{"reason":"separation of duties"}`))
	if w.Code != http.StatusForbidden {
		t.Fatalf("adverse self-approval status=%d body=%s", w.Code, w.Body)
	}
	w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-admin-e12-05-1/approve", approver, []byte(`{"reason":"second administrator approval"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("approval status=%d body=%s", w.Code, w.Body)
	}
	var approved bool
	if err = db.Pool.QueryRow(ctx, "SELECT approved FROM pricing_versions WHERE version='usd-admin-e12-05-1'").Scan(&approved); err != nil || !approved {
		t.Fatalf("approved version missing: %v", err)
	}
	show := request(http.MethodGet, "/admin/v1/pricing/drafts/usd-admin-e12-05-1?reason=review-approved", author, nil)
	if show.Code != http.StatusOK || !strings.Contains(show.Body.String(), `"status":"approved"`) {
		t.Fatalf("show approved version status=%d body=%s", show.Code, show.Body)
	}
	list := request(http.MethodGet, "/admin/v1/pricing/drafts?reason=review-list", author, nil)
	if list.Code != http.StatusOK || strings.Count(list.Body.String(), "usd-admin-e12-05-1") != 1 {
		t.Fatalf("approved version missing or duplicated in list: status=%d body=%s", list.Code, list.Body)
	}
	editApproved := request(http.MethodPatch, "/admin/v1/pricing/drafts/usd-admin-e12-05-1", author, payload)
	if editApproved.Code != http.StatusConflict {
		t.Fatalf("approved edit status=%d body=%s", editApproved.Code, editApproved.Body)
	}
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	captureMu.Lock()
	defer captureMu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("pricing lifecycle events=%d want=2: %+v", len(captured), captured)
	}
	got := map[string]bool{}
	for _, event := range captured {
		name, _ := event["event"].(string)
		got[name] = true
		actor, _ := event["distinct_id"].(string)
		properties, _ := event["properties"].(map[string]any)
		if (actor != authorID && actor != approverID) || properties["actor_id"] != actor || properties["pricing_version"] != "usd-admin-e12-05-1" || properties["source"] != "api" || properties["platform"] != "web" || properties["app_version"] == "" {
			t.Fatalf("missing UUID/common event props: %+v", event)
		}
		if _, ok := properties["reason"]; ok {
			t.Fatalf("reason leaked into analytics: %+v", event)
		}
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "approved product cost update") || strings.Contains(string(raw), "second administrator approval") {
			t.Fatalf("private reason in analytics: %s", raw)
		}
	}
	if !got["pricing_version_created"] || !got["pricing_version_approved"] {
		t.Fatalf("missing lifecycle events: %v", got)
	}
	claims, err := auth.ParseAccess(author)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE auth_sessions SET reauthenticated_at=now()-interval '1 hour' WHERE id=$1::uuid", claims.ID); err != nil {
		t.Fatal(err)
	}
	stale := request(http.MethodGet, "/admin/v1/pricing/drafts?reason=step-up-check", author, nil)
	if stale.Code != http.StatusForbidden {
		t.Fatalf("stale admin session status=%d body=%s", stale.Code, stale.Body)
	}
	var deniedCount int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_audit WHERE action='access_denied'").Scan(&deniedCount); err != nil || deniedCount < 2 {
		t.Fatalf("denied access was not audited: count=%d err=%v", deniedCount, err)
	}
}
