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

type appleFixture struct {
	identity accounts.ProviderIdentity
	keyID    string
	keys     map[string]bool
	rotated  bool
}

func (*appleFixture) Audience() string { return "com.duolify.client" }

func (p *appleFixture) AuthorizationURL(_ context.Context, request accounts.OAuthAuthorizationRequest) (string, error) {
	return "https://appleid.apple.test/authorize?state=" + request.State + "&nonce=" + request.Nonce + "&challenge=" + request.CodeChallenge, nil
}

func (p *appleFixture) Exchange(_ context.Context, request accounts.OAuthExchangeRequest) (accounts.ProviderIdentity, error) {
	if request.Code != "fixture-apple-code" || request.CodeVerifier != strings.Repeat("v", 43) || request.RedirectURI != "duolify://oauth/callback" {
		return accounts.ProviderIdentity{}, accounts.ErrOAuthProvider
	}
	if !p.keys[p.keyID] {
		// Apple adapters refresh the signing-key set once when kid is unknown.
		p.keys[p.keyID] = true
		p.rotated = true
	}
	identity := p.identity
	identity.KeyID = p.keyID
	identity.Nonce = request.Nonce
	identity.ExpiresAt = time.Now().Add(time.Hour)
	return identity, nil
}

func TestAppleOAuthClaimsRotationReturningIdentityAndDeletion(t *testing.T) {
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
	provider := &appleFixture{
		identity: accounts.ProviderIdentity{Provider: "apple", Subject: "apple-subject-fixture", Email: "relay@privaterelay.appleid.com", EmailVerified: true, Name: "Apple Learner", Issuer: "https://appleid.apple.com", Audience: "com.duolify.client"},
		keyID:    "rotated-key",
		keys:     map[string]bool{"old-key": true},
	}
	auth, err := accounts.NewWithOAuthProviders(db.Pool, strings.Repeat("test-only-key-", 4), new(accounts.MailSink), nil, provider)
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
	verifier := strings.Repeat("v", 43)
	start := func() accounts.OAuthTransaction {
		w := call("/v1/auth/apple/start", map[string]any{"redirect_uri": "duolify://oauth/callback", "code_challenge": accounts.PKCEChallenge(verifier)})
		if w.Code != 200 {
			t.Fatalf("start: %d %s", w.Code, w.Body)
		}
		var transaction accounts.OAuthTransaction
		if err := json.Unmarshal(w.Body.Bytes(), &transaction); err != nil {
			t.Fatal(err)
		}
		return transaction
	}
	complete := func(transaction accounts.OAuthTransaction) *httptest.ResponseRecorder {
		return call("/v1/auth/apple/complete", map[string]any{"state": transaction.State, "nonce": transaction.Nonce, "code": "fixture-apple-code", "code_verifier": verifier, "redirect_uri": "duolify://oauth/callback", "age_confirmed": true, "locale": "en", "time_zone": "UTC"})
	}
	first := complete(start())
	if first.Code != 200 {
		t.Fatalf("first Apple sign-in: %d %s", first.Code, first.Body)
	}
	if !provider.rotated {
		t.Fatal("new Apple signing key was not resolved through key rotation")
	}
	var learnerID string
	if err := db.Pool.QueryRow(ctx, "SELECT id::text FROM learners WHERE display_name='Apple Learner'").Scan(&learnerID); err != nil {
		t.Fatal(err)
	}
	var email string
	if err := db.Pool.QueryRow(ctx, "SELECT email FROM auth_identities WHERE learner_id=$1::uuid", learnerID).Scan(&email); err != nil || email != "relay@privaterelay.appleid.com" {
		t.Fatalf("Apple relay email not retained: %q %v", email, err)
	}
	provider.identity.Email = ""
	provider.identity.EmailVerified = false
	provider.identity.Name = ""
	returning := complete(start())
	if returning.Code != 200 {
		t.Fatalf("returning Apple identity without profile claims: %d %s", returning.Code, returning.Body)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM learners WHERE id=$1::uuid AND display_name='Apple Learner'", learnerID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("returning sign-in changed stored name: count=%d err=%v", count, err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM auth_provider_identities WHERE provider='apple' AND subject='apple-subject-fixture' AND learner_id=$1::uuid", learnerID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("Apple binding missing: count=%d err=%v", count, err)
	}
	if _, err := db.Pool.Exec(ctx, "DELETE FROM learners WHERE id=$1::uuid", learnerID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM auth_provider_identities WHERE provider='apple' AND subject='apple-subject-fixture'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("Apple binding survived learner deletion: count=%d err=%v", count, err)
	}
}

func TestAppleOAuthRejectsIssuerAudienceNonceAndEmailCollision(t *testing.T) {
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
	provider := &appleFixture{identity: accounts.ProviderIdentity{Provider: "apple", Subject: "apple-new-subject", Email: "existing@example.test", EmailVerified: true, Issuer: "https://appleid.apple.com", Audience: "com.duolify.client"}, keyID: "key-1", keys: map[string]bool{"key-1": true}}
	auth, err := accounts.NewWithOAuthProviders(db.Pool, strings.Repeat("test-only-key-", 4), new(accounts.MailSink), nil, provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO learners(id,display_name) VALUES('a1111111-1111-4111-8111-111111111111','Existing learner')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO auth_identities(email,learner_id) VALUES('existing@example.test','a1111111-1111-4111-8111-111111111111')`); err != nil {
		t.Fatal(err)
	}
	call := func(path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		request := httptest.NewRequestWithContext(ctx, "POST", path, bytes.NewReader(raw))
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, request)
		return w
	}
	verifier := strings.Repeat("v", 43)
	for _, mutate := range []func(){
		func() { provider.identity.Issuer = "https://evil.test" },
		func() { provider.identity.Audience = "wrong.client" },
	} {
		mutate()
		start := call("/v1/auth/apple/start", map[string]any{"redirect_uri": "duolify://oauth/callback", "code_challenge": accounts.PKCEChallenge(verifier)})
		var transaction accounts.OAuthTransaction
		if start.Code != 200 || json.Unmarshal(start.Body.Bytes(), &transaction) != nil {
			t.Fatalf("start: %d %s", start.Code, start.Body)
		}
		w := call("/v1/auth/apple/complete", map[string]any{"state": transaction.State, "nonce": transaction.Nonce, "code": "fixture-apple-code", "code_verifier": verifier, "redirect_uri": "duolify://oauth/callback", "age_confirmed": true, "locale": "en", "time_zone": "UTC"})
		if w.Code != 401 {
			t.Fatalf("invalid Apple issuer/audience accepted: %d %s", w.Code, w.Body)
		}
		provider.identity.Issuer = "https://appleid.apple.com"
		provider.identity.Audience = "com.duolify.client"
	}
	start := call("/v1/auth/apple/start", map[string]any{"redirect_uri": "duolify://oauth/callback", "code_challenge": accounts.PKCEChallenge(verifier)})
	var transaction accounts.OAuthTransaction
	if start.Code != 200 || json.Unmarshal(start.Body.Bytes(), &transaction) != nil {
		t.Fatalf("collision start: %d %s", start.Code, start.Body)
	}
	w := call("/v1/auth/apple/complete", map[string]any{"state": transaction.State, "nonce": strings.Repeat("x", 32), "code": "fixture-apple-code", "code_verifier": verifier, "redirect_uri": "duolify://oauth/callback", "age_confirmed": true, "locale": "en", "time_zone": "UTC"})
	if w.Code != 401 {
		t.Fatalf("nonce mismatch accepted: %d %s", w.Code, w.Body)
	}
	w = call("/v1/auth/apple/complete", map[string]any{"state": transaction.State, "nonce": transaction.Nonce, "code": "fixture-apple-code", "code_verifier": verifier, "redirect_uri": "duolify://oauth/callback", "age_confirmed": true, "locale": "en", "time_zone": "UTC"})
	if w.Code != 409 {
		t.Fatalf("email collision silently linked: %d %s", w.Code, w.Body)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM auth_provider_identities WHERE provider='apple' AND subject='apple-new-subject'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("collision created binding: count=%d err=%v", count, err)
	}
	provider.identity.Subject = "apple-without-email"
	provider.identity.Email = ""
	provider.identity.EmailVerified = false
	start = call("/v1/auth/apple/start", map[string]any{"redirect_uri": "duolify://oauth/callback", "code_challenge": accounts.PKCEChallenge(verifier)})
	if start.Code != 200 || json.Unmarshal(start.Body.Bytes(), &transaction) != nil {
		t.Fatalf("missing-email start: %d %s", start.Code, start.Body)
	}
	w = call("/v1/auth/apple/complete", map[string]any{"state": transaction.State, "nonce": transaction.Nonce, "code": "fixture-apple-code", "code_verifier": verifier, "redirect_uri": "duolify://oauth/callback", "age_confirmed": true, "locale": "en", "time_zone": "UTC"})
	if w.Code != 401 {
		t.Fatalf("new Apple subject without verified email accepted: %d %s", w.Code, w.Body)
	}
}
