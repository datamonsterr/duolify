//go:build integration

package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestOptionalContextConfirmationOwnershipAndEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("profiles"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	var mu sync.Mutex
	var captured []map[string]any
	sinkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		captured = append(captured, body.Batch...)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer sinkServer.Close()
	events, err := analytics.New("test-only", sinkServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()
	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key", 5), mail)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body any, token string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(raw))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, r)
		return w
	}
	// Endpoints must exist and authenticate before touching learner data.
	if w := call("GET", "/v1/me/profile", nil, ""); w.Code != 401 {
		t.Fatalf("anonymous profile: %d", w.Code)
	}
	auth.ConfigureProfile(accounts.ProfileOptions{TextLimit: 20, Events: events})
	login := func(email string, optOut bool) accounts.Tokens {
		in := map[string]any{"email": email, "age_confirmed": true, "locale": "vi-VN", "time_zone": "Asia/Ho_Chi_Minh", "analytics_opt_out": optOut}
		if w := call("POST", "/v1/auth/code", in, ""); w.Code != 202 {
			t.Fatalf("request: %d %s", w.Code, w.Body)
		}
		in["code"] = mail.Code(email)
		w := call("POST", "/v1/auth/verify", in, "")
		if w.Code != 200 {
			t.Fatalf("verify: %d %s", w.Code, w.Body)
		}
		var tokens accounts.Tokens
		if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
			t.Fatal(err)
		}
		return tokens
	}
	first, other := login("profile@example.test", false), login("other@example.test", true)
	read := func(token string) map[string]any {
		w := call("GET", "/v1/me/profile", nil, token)
		if w.Code != 200 {
			t.Fatalf("read: %d %s", w.Code, w.Body)
		}
		var p map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	initial := read(first.AccessToken)
	if initial["locale"] != "vi-VN" || initial["time_zone"] != "Asia/Ho_Chi_Minh" || initial["background"] != "" || initial["daily_minutes"] != nil || initial["setup_completed"] != false {
		t.Fatalf("defaults: %v", initial)
	}
	save := func(token string, confirmed bool, action string, fields map[string]any) *httptest.ResponseRecorder {
		return call("POST", "/v1/me/profile", map[string]any{"confirmed": confirmed, "action": action, "fields": fields}, token)
	}
	if w := save(first.AccessToken, false, "save", map[string]any{"goal": "private draft"}); w.Code != 400 {
		t.Fatalf("unconfirmed: %d", w.Code)
	}
	if read(first.AccessToken)["goal"] != "" {
		t.Fatal("draft persisted")
	}
	for _, fields := range []map[string]any{{"background": strings.Repeat("界", 21)}, {"time_zone": "Mars/Base"}, {"daily_minutes": 0}, {"daily_xp": 10001}, {"level": "wizard"}, {"reminder_time": "25:00"}, {"learner_id": initial["learner_id"]}, {"analytics_opt_out": true}, {"goal": nil}} {
		if w := save(first.AccessToken, true, "save", fields); w.Code != 400 {
			t.Fatalf("invalid %v: %d %s", fields, w.Code, w.Body)
		}
	}
	fields := map[string]any{"goal": "private goal", "topic": "private topic", "background": strings.Repeat("界", 20), "level": "beginner", "daily_minutes": 10, "daily_xp": 20, "reminder_enabled": false, "reminder_time": "09:30"}
	if w := save(first.AccessToken, true, "save", fields); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if read(other.AccessToken)["goal"] != "" {
		t.Fatal("cross-owner write")
	}
	if w := save(first.AccessToken, true, "save", map[string]any{"background": "", "daily_minutes": nil, "locale": "", "time_zone": ""}); w.Code != 200 {
		t.Fatalf("clear: %d %s", w.Code, w.Body)
	}
	p := read(first.AccessToken)
	if p["goal"] != "private goal" || p["background"] != "" || p["daily_minutes"] != nil || p["locale"] != "" || p["time_zone"] != "" {
		t.Fatalf("clear/partial: %v", p)
	}
	var optedOut, ageConfirmed bool
	if err := db.Pool.QueryRow(ctx, "SELECT analytics_opt_out,age_confirmed_at IS NOT NULL FROM learner_profiles WHERE goal='private goal'").Scan(&optedOut, &ageConfirmed); err != nil || optedOut || !ageConfirmed {
		t.Fatal("unrelated preference changed", err)
	}
	export := call("GET", "/v1/me/profile/export", nil, first.AccessToken)
	if export.Code != 200 || !strings.Contains(export.Body.String(), "private goal") || export.Header().Get("Content-Disposition") == "" {
		t.Fatal("export failed")
	}
	if w := save(first.AccessToken, true, "skip", map[string]any{"goal": "discard"}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if read(first.AccessToken)["goal"] != "private goal" {
		t.Fatal("skip saved draft")
	}
	if w := save(other.AccessToken, true, "skip", map[string]any{"goal": "discard"}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if read(other.AccessToken)["goal"] != "" {
		t.Fatal("skip saved answers")
	}
	if w := save(other.AccessToken, true, "save", map[string]any{"goal": "opt out private"}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	counts := map[string]int{}
	for _, event := range captured {
		name, _ := event["event"].(string)
		counts[name]++
		props, _ := event["properties"].(map[string]any)
		if event["distinct_id"] != initial["learner_id"] || props["source"] != "api" || props["platform"] != "web" || props["app_version"] == "" {
			t.Fatalf("common properties: %v", event)
		}
		raw, _ := json.Marshal(event)
		for _, private := range []string{"private goal", "private topic", "界", "discard", "opt out private", "@example.test"} {
			if strings.Contains(string(raw), private) {
				t.Fatal("private text in analytics")
			}
		}
		if name == "learner_profile_updated" {
			if _, ok := props["fields_changed"].([]any); !ok {
				t.Fatal("missing changed names")
			}
		}
	}
	if counts["onboarding_completed"] != 1 || counts["learner_profile_updated"] != 2 {
		t.Fatalf("events: %v", counts)
	}
	// Account erasure cascades current profile; no history survives.
	if _, err := db.Pool.Exec(ctx, "DELETE FROM learners WHERE id::text=$1", initial["learner_id"]); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM learner_profiles WHERE learner_id::text=$1", initial["learner_id"]).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("context survived deletion", err)
	}
}
