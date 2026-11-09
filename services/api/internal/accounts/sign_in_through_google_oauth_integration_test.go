//go:build integration

package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type googleFixture struct {
	subject string
	email   string
}

func (p *googleFixture) AuthorizationURL(_ context.Context, request accounts.OAuthAuthorizationRequest) (string, error) {
	return "https://accounts.google.test/authorize?state=" + request.State + "&nonce=" + request.Nonce + "&challenge=" + request.CodeChallenge, nil
}

func (p *googleFixture) Exchange(_ context.Context, request accounts.OAuthExchangeRequest) (accounts.ProviderIdentity, error) {
	if request.Code != "fixture-code" || request.CodeVerifier != strings.Repeat("v", 43) || request.RedirectURI != "duolify://oauth/callback" {
		return accounts.ProviderIdentity{}, accounts.ErrOAuthProvider
	}
	return accounts.ProviderIdentity{Provider: "google", Subject: p.subject, Email: p.email, EmailVerified: true, Nonce: request.Nonce}, nil
}

func TestGoogleOAuthReplayAndVerifiedEmailCollision(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	provider := &googleFixture{subject: "google-subject-fixture", email: "oauth@example.test"}
	auth, err := accounts.NewWithOAuthProvider(db.Pool, strings.Repeat("test-only-key-", 4), new(accounts.MailSink), provider)
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		request := httptest.NewRequestWithContext(ctx, "POST", path, bytes.NewReader(raw))
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, request)
		return w
	}
	if rejected := call("/v1/auth/google/start", map[string]any{"redirect_uri": "https://evil.test/callback", "code_challenge": accounts.PKCEChallenge(strings.Repeat("v", 43))}); rejected.Code != 400 {
		t.Fatalf("unsafe redirect accepted: %d %s", rejected.Code, rejected.Body)
	}
	verifier := strings.Repeat("v", 43)
	start := call("/v1/auth/google/start", map[string]any{"redirect_uri": "duolify://oauth/callback", "code_challenge": accounts.PKCEChallenge(verifier)})
	if start.Code != 200 {
		t.Fatalf("start: %d %s", start.Code, start.Body)
	}
	var transaction accounts.OAuthTransaction
	if err := json.Unmarshal(start.Body.Bytes(), &transaction); err != nil {
		t.Fatal(err)
	}
	complete := map[string]any{"state": transaction.State, "nonce": transaction.Nonce, "code": "fixture-code", "code_verifier": verifier, "redirect_uri": "duolify://oauth/callback", "age_confirmed": true, "locale": "en", "time_zone": "UTC"}
	wrongNonce := map[string]any{}
	for key, value := range complete {
		wrongNonce[key] = value
	}
	wrongNonce["nonce"] = strings.Repeat("n", 32)
	if rejected := call("/v1/auth/google/complete", wrongNonce); rejected.Code != 401 {
		t.Fatalf("nonce substitution accepted: %d %s", rejected.Code, rejected.Body)
	}
	wrongVerifier := map[string]any{}
	for key, value := range complete {
		wrongVerifier[key] = value
	}
	wrongVerifier["code_verifier"] = strings.Repeat("x", 43)
	if rejected := call("/v1/auth/google/complete", wrongVerifier); rejected.Code != 401 {
		t.Fatalf("PKCE substitution accepted: %d %s", rejected.Code, rejected.Body)
	}
	first := call("/v1/auth/google/complete", complete)
	if first.Code != 200 {
		t.Fatalf("complete: %d %s", first.Code, first.Body)
	}
	var tokens accounts.Tokens
	if err := json.Unmarshal(first.Body.Bytes(), &tokens); err != nil || tokens.RefreshToken == "" {
		t.Fatalf("tokens missing: %v", err)
	}
	if replay := call("/v1/auth/google/complete", complete); replay.Code != 401 {
		t.Fatalf("replay accepted: %d %s", replay.Code, replay.Body)
	}
	var bindings int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM auth_provider_identities WHERE provider='google' AND subject='google-subject-fixture'").Scan(&bindings); err != nil || bindings != 1 {
		t.Fatalf("provider binding count %d: %v", bindings, err)
	}
	var learnerID string
	if err := db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_provider_identities WHERE provider='google' AND subject='google-subject-fixture'").Scan(&learnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "DELETE FROM learners WHERE id=$1::uuid", learnerID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM auth_provider_identities WHERE subject='google-subject-fixture'").Scan(&bindings); err != nil || bindings != 0 {
		t.Fatalf("provider binding survived learner deletion: %d %v", bindings, err)
	}
	// A different verified provider subject may not silently acquire an email identity.
	if _, err := db.Pool.Exec(ctx, `INSERT INTO learners(id,display_name) VALUES('a1111111-1111-4111-8111-111111111111','Existing learner')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO auth_identities(email,learner_id) VALUES('collision@example.test','a1111111-1111-4111-8111-111111111111')`); err != nil {
		t.Fatal(err)
	}
	provider.subject = "second-google-subject"
	provider.email = "collision@example.test"
	start = call("/v1/auth/google/start", map[string]any{"redirect_uri": "duolify://oauth/callback", "code_challenge": accounts.PKCEChallenge(verifier)})
	if start.Code != 200 {
		t.Fatalf("collision start: %d %s", start.Code, start.Body)
	}
	if err := json.Unmarshal(start.Body.Bytes(), &transaction); err != nil {
		t.Fatal(err)
	}
	complete = map[string]any{"state": transaction.State, "nonce": transaction.Nonce, "code": "fixture-code", "code_verifier": verifier, "redirect_uri": "duolify://oauth/callback", "age_confirmed": true, "locale": "en", "time_zone": "UTC"}
	if collision := call("/v1/auth/google/complete", complete); collision.Code != 409 {
		t.Fatalf("verified email collision merged: %d %s", collision.Code, collision.Body)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM auth_provider_identities WHERE subject='second-google-subject'").Scan(&bindings); err != nil || bindings != 0 {
		t.Fatalf("collision created provider binding: %d %v", bindings, err)
	}
}
