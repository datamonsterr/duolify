package accounts

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/golang-jwt/jwt/v5"
)

type fakeClerkUsers struct{ user *clerk.User }

func (f fakeClerkUsers) Get(context.Context, string) (*clerk.User, error) {
	if f.user == nil {
		return nil, errors.New("not found")
	}
	return f.user, nil
}

func clerkUser(id, email, status string) *clerk.User {
	emailID := "idn_1"
	first := "Ada"
	return &clerk.User{ID: id, FirstName: &first, PrimaryEmailAddressID: &emailID, EmailAddresses: []*clerk.EmailAddress{{ID: emailID, EmailAddress: email, Verification: &clerk.Verification{Status: status, Strategy: "from_oauth_google"}}}}
}

func TestClerkVerifierChecksSignatureIssuerPartyAndEmailVerification(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	sign := func(signer *rsa.PrivateKey, issuer, party string, expires time.Time) string {
		claims := jwt.MapClaims{"iss": issuer, "sub": "user_1", "azp": party, "iat": time.Now().Add(-time.Minute).Unix(), "nbf": time.Now().Add(-time.Minute).Unix(), "exp": expires.Unix()}
		token, signErr := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(signer)
		if signErr != nil {
			t.Fatal(signErr)
		}
		return token
	}
	build := func(user *clerk.User) ClerkVerifier {
		verifier, buildErr := NewClerkVerifier(ClerkConfig{SecretKey: "sk_test_fixture", JWTKey: pemKey, AuthorizedParties: []string{"https://app.duolify.test"}, Users: fakeClerkUsers{user}})
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		return verifier
	}
	ctx := t.Context()
	good := sign(key, "https://clerk.duolify.test", "https://app.duolify.test", time.Now().Add(time.Minute))
	identity, err := build(clerkUser("user_1", "ada@example.test", "verified")).Verify(ctx, good)
	if err != nil || identity.UserID != "user_1" || identity.Email != "ada@example.test" || !identity.EmailVerified || identity.Name != "Ada" {
		t.Fatalf("valid token: %+v %v", identity, err)
	}
	if identity, err = build(clerkUser("user_1", "ada@example.test", "unverified")).Verify(ctx, good); err != nil || identity.EmailVerified {
		t.Fatalf("unverified email must be reported unverified: %+v %v", identity, err)
	}
	for _, strategy := range []string{"admin", "ticket", "email_link", ""} {
		user := clerkUser("user_1", "ada@example.test", "verified")
		user.EmailAddresses[0].Verification.Strategy = strategy
		if identity, err = build(user).Verify(ctx, good); err != nil || identity.EmailVerified {
			t.Fatalf("strategy %q must not count as verified: %+v %v", strategy, identity, err)
		}
	}
	secondary := clerkUser("user_1", "ada@example.test", "verified")
	secondary.EmailAddresses = append(secondary.EmailAddresses, &clerk.EmailAddress{ID: "idn_2", EmailAddress: "victim@example.test", Verification: &clerk.Verification{Status: "verified", Strategy: "email_code"}})
	if identity, err = build(secondary).Verify(ctx, good); err != nil || identity.Email != "ada@example.test" {
		t.Fatalf("only the primary email may be used: %+v %v", identity, err)
	}
	banned := clerkUser("user_1", "ada@example.test", "verified")
	banned.Banned = true
	for name, tc := range map[string]struct {
		token string
		user  *clerk.User
	}{
		"wrong signing key": {sign(other, "https://clerk.duolify.test", "https://app.duolify.test", time.Now().Add(time.Minute)), clerkUser("user_1", "a@example.test", "verified")},
		"foreign issuer":    {sign(key, "https://evil.test", "https://app.duolify.test", time.Now().Add(time.Minute)), clerkUser("user_1", "a@example.test", "verified")},
		"foreign party":     {sign(key, "https://clerk.duolify.test", "https://evil.test", time.Now().Add(time.Minute)), clerkUser("user_1", "a@example.test", "verified")},
		"missing party":     {sign(key, "https://clerk.duolify.test", "", time.Now().Add(time.Minute)), clerkUser("user_1", "a@example.test", "verified")},
		"expired":           {sign(key, "https://clerk.duolify.test", "https://app.duolify.test", time.Now().Add(-time.Hour)), clerkUser("user_1", "a@example.test", "verified")},
		"subject mismatch":  {good, clerkUser("user_2", "a@example.test", "verified")},
		"banned user":       {good, banned},
		"garbage":           {"not-a-jwt", clerkUser("user_1", "a@example.test", "verified")},
		"empty":             {"", clerkUser("user_1", "a@example.test", "verified")},
	} {
		if _, err := build(tc.user).Verify(ctx, tc.token); err == nil {
			t.Errorf("%s accepted", name)
		} else if name != "subject mismatch" && !errors.Is(err, ErrClerkVerification) {
			t.Errorf("%s returned a non-verification error: %v", name, err)
		}
	}
}

func TestClerkFromEnvFailsClosed(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	if verifier, key, err := ClerkFromEnv(env(nil)); err != nil || verifier != nil || key != "" {
		t.Fatalf("no keys must disable SSO: %v %q %v", verifier, key, err)
	}
	for name, values := range map[string]map[string]string{
		"secret only":       {"CLERK_SECRET_KEY": "sk_test_x"},
		"publishable only":  {"CLERK_PUBLISHABLE_KEY": "pk_test_x"},
		"no parties":        {"CLERK_SECRET_KEY": "sk_test_x", "CLERK_PUBLISHABLE_KEY": "pk_test_x"},
		"bad secret":        {"CLERK_SECRET_KEY": "nope", "CLERK_PUBLISHABLE_KEY": "pk_test_x", "CLERK_AUTHORIZED_PARTIES": "https://a.test"},
		"bad publishable":   {"CLERK_SECRET_KEY": "sk_test_x", "CLERK_PUBLISHABLE_KEY": "nope", "CLERK_AUTHORIZED_PARTIES": "https://a.test"},
		"malformed jwt key": {"CLERK_SECRET_KEY": "sk_test_x", "CLERK_PUBLISHABLE_KEY": "pk_test_x", "CLERK_AUTHORIZED_PARTIES": "https://a.test", "CLERK_JWT_KEY": "junk"},
	} {
		if _, _, err := ClerkFromEnv(env(values)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	ok := map[string]string{"CLERK_SECRET_KEY": "sk_test_x", "CLERK_PUBLISHABLE_KEY": "pk_test_x", "CLERK_AUTHORIZED_PARTIES": "https://a.test, https://b.test"}
	if verifier, key, err := ClerkFromEnv(env(ok)); err != nil || verifier == nil || key != "pk_test_x" {
		t.Fatalf("valid config: %v %q %v", verifier, key, err)
	}
}

func TestPKCEHelpers(t *testing.T) {
	verifier := "v" + string(make([]byte, 0)) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if !validPKCEVerifier(verifier) || validPKCEVerifier("short") || validPKCEVerifier(verifier+"=") {
		t.Fatal("verifier validation")
	}
	if !ValidPKCEChallenge(PKCEChallenge(verifier)) || ValidPKCEChallenge("x") {
		t.Fatal("challenge validation")
	}
	if !ValidSSORedirect("duolify://sso/callback") || ValidSSORedirect("duolify://oauth/callback") || ValidSSORedirect("https://evil.test") {
		t.Fatal("redirect allowlist")
	}
}
