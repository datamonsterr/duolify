package accounts_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

func TestAppleOAuthClaimBoundaries(t *testing.T) {
	valid := accounts.ProviderIdentity{Provider: "apple", Subject: "subject", Email: "relay@privaterelay.appleid.com", EmailVerified: true, Nonce: "nonce", Issuer: "https://appleid.apple.com", Audience: "com.duolify.client", KeyID: "fixture-key", ExpiresAt: time.Now().Add(time.Hour)}
	if err := accounts.ValidateAppleIdentity(valid, "nonce", "com.duolify.client"); err != nil {
		t.Fatal(err)
	}
	googleWithoutEmail := accounts.ProviderIdentity{Provider: "google", Subject: "subject", Nonce: "nonce"}
	if err := accounts.ValidateProviderIdentity(googleWithoutEmail, "nonce"); err == nil {
		t.Fatal("Google verified-email contract was weakened")
	}
	// Returning Apple sign-ins may omit the email claim; subject still identifies the binding.
	returning := valid
	returning.Email = ""
	returning.EmailVerified = false
	if err := accounts.ValidateAppleIdentity(returning, "nonce", "com.duolify.client"); err != nil {
		t.Fatalf("returning identity with omitted email rejected: %v", err)
	}
	for name, identity := range map[string]accounts.ProviderIdentity{
		"wrong issuer":           func() accounts.ProviderIdentity { v := valid; v.Issuer = "https://evil.test"; return v }(),
		"wrong audience":         func() accounts.ProviderIdentity { v := valid; v.Audience = "other"; return v }(),
		"wrong nonce":            func() accounts.ProviderIdentity { v := valid; v.Nonce = "other"; return v }(),
		"missing signing key id": func() accounts.ProviderIdentity { v := valid; v.KeyID = ""; return v }(),
		"unverified email":       func() accounts.ProviderIdentity { v := valid; v.EmailVerified = false; return v }(),
		"empty subject":          func() accounts.ProviderIdentity { v := valid; v.Subject = ""; return v }(),
		"expired identity token": func() accounts.ProviderIdentity { v := valid; v.ExpiresAt = time.Now().Add(-time.Second); return v }(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := accounts.ValidateAppleIdentity(identity, "nonce", "com.duolify.client"); err == nil {
				t.Fatal("invalid Apple claims accepted")
			}
		})
	}
}

func TestAppleClientSecretSignerHasFixtureOnly(t *testing.T) {
	secret, err := (accounts.FixtureAppleClientSecret{}).SignClientSecret(t.Context())
	if err != nil || secret != "fixture.apple.client-secret" {
		t.Fatalf("fixture client secret=%q err=%v", secret, err)
	}
}

func TestAppleOAuthFixtureAndUnavailableDefault(t *testing.T) {
	provider := &accounts.FixtureAppleProvider{}
	request := accounts.OAuthAuthorizationRequest{State: "state", Nonce: "nonce", CodeChallenge: strings.Repeat("a", 43), RedirectURI: "duolify://oauth/callback"}
	url, err := provider.AuthorizationURL(t.Context(), request)
	if err != nil || !strings.HasPrefix(url, "https://appleid.apple.test/") {
		t.Fatalf("fixture authorization url=%q err=%v", url, err)
	}
	identity, err := provider.Exchange(t.Context(), accounts.OAuthExchangeRequest{Code: "fixture-apple-code", CodeVerifier: strings.Repeat("v", 43), RedirectURI: "duolify://oauth/callback", Nonce: "nonce"})
	if err != nil || identity.Provider != "apple" || identity.Subject == "" {
		t.Fatalf("fixture identity=%+v err=%v", identity, err)
	}
	auth, err := accounts.New(nil, strings.Repeat("test-only-key-", 4), new(accounts.MailSink))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"redirect_uri": "duolify://oauth/callback", "code_challenge": accounts.PKCEChallenge(strings.Repeat("v", 43))})
	requestHTTP := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/auth/apple/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	auth.ServeHTTP(w, requestHTTP)
	if w.Code != 503 {
		t.Fatalf("unprovisioned Apple provider enabled: %d %s", w.Code, w.Body)
	}
}
