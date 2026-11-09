package accounts_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

func TestGoogleOAuthBoundaries(t *testing.T) {
	for _, redirect := range []string{"duolify://oauth/callback", "https://evil.test/callback", "duolify://oauth/callback/extra", "duolify://oauth/callback?next=https://evil.test"} {
		if accounts.ValidOAuthRedirect(redirect) != (redirect == "duolify://oauth/callback") {
			t.Errorf("redirect allowlist mismatch for %q", redirect)
		}
	}
	if !accounts.ValidPKCEChallenge(strings.Repeat("a", 43)) || accounts.ValidPKCEChallenge(strings.Repeat("a", 42)+"+") {
		t.Fatal("invalid PKCE challenge accepted")
	}
	challenge := accounts.PKCEChallenge("verifier-fixture")
	if len(challenge) != 43 || challenge == "verifier-fixture" || challenge != accounts.PKCEChallenge("verifier-fixture") {
		t.Fatal("PKCE challenge does not use SHA-256/base64url")
	}
	valid := accounts.ProviderIdentity{Provider: "google", Subject: "subject", Email: "learner@example.test", EmailVerified: true, Nonce: "nonce"}
	if err := accounts.ValidateProviderIdentity(valid, "nonce"); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []accounts.ProviderIdentity{
		{Provider: "google", Subject: "subject", Email: "learner@example.test", EmailVerified: false, Nonce: "nonce"},
		{Provider: "unknown", Subject: "subject", Email: "learner@example.test", EmailVerified: true, Nonce: "nonce"},
		{Provider: "google", Subject: strings.Repeat("x", 300), Email: "learner@example.test", EmailVerified: true, Nonce: "nonce"},
	} {
		if err := accounts.ValidateProviderIdentity(identity, "nonce"); err == nil {
			t.Errorf("invalid provider identity accepted: %+v", identity)
		}
	}
	if err := accounts.ValidateProviderIdentity(valid, "other-nonce"); err == nil {
		t.Fatal("nonce substitution accepted")
	}
}

func TestGoogleOAuthRemainsUnavailableUntilProvisioned(t *testing.T) {
	auth, err := accounts.New(nil, strings.Repeat("test-only-key-", 4), new(accounts.MailSink))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{
		"redirect_uri":   "duolify://oauth/callback",
		"code_challenge": accounts.PKCEChallenge(strings.Repeat("v", 43)),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/v1/auth/google/start", bytes.NewReader(body))
	response := httptest.NewRecorder()
	auth.ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatalf("unprovisioned provider enabled: %d %s", response.Code, response.Body)
	}
}
