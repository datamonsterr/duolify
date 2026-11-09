//go:build integration

package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"io"
	"log/slog"
)

func TestRotateAndRevokeSessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("sessions"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	sink := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key-", 5), sink)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(httpapi.Dependencies{Accounts: auth}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	call := func(method, path string, body any, token string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	login := func(email string) accounts.Tokens {
		t.Helper()
		input := map[string]any{"email": email, "age_confirmed": true}
		if w := call("POST", "/v1/auth/code", input, ""); w.Code != 202 {
			t.Fatal(w.Code, w.Body)
		}
		input["code"] = sink.Code(email)
		w := call("POST", "/v1/auth/verify", input, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		var tokens accounts.Tokens
		if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
			t.Fatal(err)
		}
		return tokens
	}
	first, other, foreign := login("owner@example.test"), login("owner@example.test"), login("foreign@example.test")
	w := call("GET", "/v1/auth/sessions", nil, first.AccessToken)
	if w.Code != 200 {
		t.Fatalf("list sessions: %d %s", w.Code, w.Body)
	}
	var rows []struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil || len(rows) != 2 {
		t.Fatal("owned session list", w.Body, err)
	}
	if strings.Contains(w.Body.String(), first.RefreshToken) || strings.Contains(w.Body.String(), "@") {
		t.Fatal("secret in listing")
	}
	var target string
	for _, row := range rows {
		if !row.Current {
			target = row.ID
		}
	}
	if target == "" {
		t.Fatal("missing other device")
	}
	if w := call("DELETE", "/v1/auth/sessions/"+target, nil, foreign.AccessToken); w.Code != 404 {
		t.Fatal("foreign revocation", w.Code)
	}
	if w := call("DELETE", "/v1/auth/sessions/"+target, nil, first.AccessToken); w.Code != 204 {
		t.Fatal("revoke device", w.Code)
	}
	if w := call("GET", "/v1/me", nil, other.AccessToken); w.Code != 401 {
		t.Fatal("revoked cached access", w.Code)
	}
	if w := call("POST", "/v1/auth/refresh", map[string]string{"refresh_token": other.RefreshToken}, ""); w.Code != 401 {
		t.Fatal("revoked refresh", w.Code)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	var rotated accounts.Tokens
	for range 8 {
		wg.Go(func() {
			w := call("POST", "/v1/auth/refresh", map[string]string{"refresh_token": first.RefreshToken}, "")
			if w.Code == 200 {
				wins.Add(1)
				if err := json.Unmarshal(w.Body.Bytes(), &rotated); err != nil {
					t.Error(err)
				}
			} else if w.Code != 401 {
				t.Error("refresh race", w.Code, w.Body)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("refresh winners", wins.Load())
	}
	if w := call("GET", "/v1/me", nil, rotated.AccessToken); w.Code != 401 {
		t.Fatal("reuse failed to revoke winner", w.Code)
	}
	if w := call("POST", "/v1/auth/refresh", map[string]string{"refresh_token": rotated.RefreshToken}, ""); w.Code != 401 {
		t.Fatal("family still active", w.Code)
	}
	fresh := login("owner@example.test")
	if w := call("DELETE", "/v1/auth/sessions", nil, fresh.AccessToken); w.Code != 204 {
		t.Fatal("logout all", w.Code)
	}
	if w := call("GET", "/v1/me", nil, fresh.AccessToken); w.Code != 401 {
		t.Fatal("logout all cached access", w.Code)
	}
	if w := call("GET", "/v1/me", nil, foreign.AccessToken); w.Code != 200 {
		t.Fatal("logout all crossed owner", w.Code)
	}
	claims, err := auth.ParseAccess(fresh.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "DELETE FROM learners WHERE id=$1::uuid", claims.Subject); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM auth_refresh_tokens t JOIN auth_sessions s ON s.id=t.session_id WHERE s.learner_id=$1::uuid`, claims.Subject).Scan(&count); err != nil || count != 0 {
		t.Fatal("family deletion", count, err)
	}
}
