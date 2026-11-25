//go:build integration

package chat

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/accountstest"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestEdgeRevokesChannelAfterSessionErasure(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("edge_chat"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	mail := new(accounts.MailSink)
	auth, err := accounts.New(pool, strings.Repeat("access-key", 4), mail)
	if err != nil {
		t.Fatal(err)
	}
	auth.ConfigureDeletionLedger(accountstest.NewDeletionLedger())
	const origin = "https://client.example.test"
	const email = "edge-erase@example.test"
	login := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(body)))
		return w
	}
	if w := login("/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`); w.Code != 202 {
		t.Fatalf("request code: %d %s", w.Code, w.Body)
	}
	w := login("/v1/auth/verify", `{"email":"`+email+`","age_confirmed":true,"code":"`+mail.Code(email)+`"}`)
	if w.Code != 200 {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	var tokens accounts.Tokens
	if err := strictEdgeJSON(w.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	claims, err := auth.ParseAccess(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("edge-key", 4)
	edge := NewEdge(EdgeOptions{Secret: secret, Tutor: New(Options{Pool: pool}), SessionActive: auth.ActiveSession, GuidedSetup: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accounts.LearnerID(r.Context()) != claims.Subject {
			t.Error("verified learner context missing")
		}
		w.WriteHeader(204)
	})})
	mux := http.NewServeMux()
	edge.MountRoutes(mux, auth.Protect, func(got string) bool { return got == origin })
	scope := edgeScope{LearnerID: claims.Subject, SessionID: claims.ID, Origin: origin, Surface: "setup", ExpiresAt: time.Now().Add(time.Minute).Unix()}
	path := "/internal/edge/chat/guided-turn"
	body := []byte(`{"current_step":"goal","answer":"learn acoustics"}`)
	call := func(key string, exp time.Time) int {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(body))
		token, err := signEdge(key, edgeServiceDomain, edgeServiceClaims{Exp: exp.Unix(), Method: http.MethodPost, Path: path, BodySHA256: bodyDigest(body), Transport: "websocket", Scope: &scope})
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w.Code
	}
	now := time.Now()
	for _, tt := range []struct {
		name, key string
		exp       time.Time
		want      int
	}{
		{"valid", secret, now.Add(25 * time.Second), 204},
		{"expired", secret, now.Add(-time.Second), 401},
		{"wrong signature", strings.Repeat("wrong-key", 4), now.Add(25 * time.Second), 401},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := call(tt.key, tt.exp); got != tt.want {
				t.Fatalf("status = %d, want %d", got, tt.want)
			}
		})
	}
	deletion := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/me/deletion-request", strings.NewReader(`{"confirmation":"DELETE"}`))
	deletion.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	deleted := httptest.NewRecorder()
	auth.ServeHTTP(deleted, deletion)
	if deleted.Code != http.StatusOK {
		t.Fatalf("account erasure: %d %s", deleted.Code, deleted.Body)
	}
	if got := call(secret, time.Now().Add(25*time.Second)); got != 401 {
		t.Fatalf("erased session remains authorized: %d", got)
	}
}
