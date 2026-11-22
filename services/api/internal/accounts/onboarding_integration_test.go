//go:build integration

package accounts_test

import (
	"context"
	"encoding/json"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOnboardingPersistsPerLearnerAndExportsErases(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("onboarding"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key", 5), mail)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, r)
		return w
	}
	login := func(email string) string {
		if w := call("POST", "/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`, ""); w.Code != 202 {
			t.Fatalf("code: %d %s", w.Code, w.Body)
		}
		w := call("POST", "/v1/auth/verify", `{"email":"`+email+`","code":"`+mail.Code(email)+`","age_confirmed":true}`, "")
		var tokens accounts.Tokens
		if w.Code != 200 {
			t.Fatalf("login: %d %s", w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
			t.Fatal(err)
		}
		return tokens.AccessToken
	}
	if w := call("GET", "/v1/me/onboarding", "", ""); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	first, other := login("tour@example.test"), login("other-tour@example.test")
	read := func(token, welcome, tour string) {
		t.Helper()
		w := call("GET", "/v1/me/onboarding", "", token)
		var state map[string]string
		if w.Code != 200 {
			t.Fatalf("read: %d %s", w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state["welcome_state"] != welcome || state["tour_state"] != tour {
			t.Fatalf("state: %v", state)
		}
	}
	read(first, "pending", "pending")
	for _, body := range []string{`{"welcome_state":"nope"}`, `{"welcome_state":null}`, `{"tour_state":"pending"}`, `{"learner_id":"other"}`, `{} {}`, `{}`} {
		if w := call("PATCH", "/v1/me/onboarding", body, first); w.Code != 400 {
			t.Fatalf("invalid %s: %d", body, w.Code)
		}
	}
	if w := call("PATCH", "/v1/me/onboarding", `{"welcome_state":"skipped"}`, first); w.Code != 200 {
		t.Fatalf("skip: %d %s", w.Code, w.Body)
	}
	read(first, "skipped", "pending")
	read(other, "pending", "pending")
	if w := call("PATCH", "/v1/me/onboarding", `{"tour_state":"completed"}`, first); w.Code != 200 {
		t.Fatalf("complete: %d", w.Code)
	}
	if w := call("PATCH", "/v1/me/onboarding", `{"tour_state":"skipped"}`, first); w.Code != 200 {
		t.Fatalf("stale skip: %d", w.Code)
	}
	read(login("tour@example.test"), "skipped", "completed")
	exported := call("GET", "/v1/me/data-export", "", first)
	if exported.Code != 200 || !strings.Contains(exported.Body.String(), `"welcome_state":"skipped"`) || !strings.Contains(exported.Body.String(), `"tour_state":"completed"`) {
		t.Fatalf("export: %d %s", exported.Code, exported.Body)
	}
	if _, err = db.Pool.Exec(ctx, `DELETE FROM learners WHERE id=(SELECT learner_id FROM auth_identities WHERE email='tour@example.test')`); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM learner_profiles WHERE welcome_state='skipped'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("erasure: %d %v", remaining, err)
	}
}
