package e2e

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func signedIn(t *testing.T) (*accounts.Service, string) {
	t.Helper()
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	sink := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, "test-only-signing-key-at-least-32-bytes", sink)
	if err != nil {
		t.Fatal(err)
	}
	auth.ConfigureRateLimits(accounts.RateLimits{RequestIP: 100000, RequestEmail: 100000, VerifyIP: 100000})
	call := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, httptest.NewRequestWithContext(ctx, "POST", path, strings.NewReader(body)))
		return w
	}
	if w := call("/v1/auth/code", `{"email":"http@example.test","age_confirmed":true}`); w.Code != 202 {
		t.Fatal(w.Body)
	}
	w := call("/v1/auth/verify", `{"email":"http@example.test","age_confirmed":true,"code":"`+sink.Code("http@example.test")+`"}`)
	var tokens accounts.Tokens
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	return auth, tokens.AccessToken
}
