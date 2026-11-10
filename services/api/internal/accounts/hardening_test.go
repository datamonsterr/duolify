package accounts

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTrustedProxyClientIP(t *testing.T) {
	for _, tt := range []struct {
		name, forwarded, want string
		hops                  int
	}{
		{"default ignores spoof", "198.51.100.1", "192.0.2.10", 0},
		{"one hop", "198.51.100.1", "198.51.100.1", 1},
		{"ignores spoofed left prefix", "203.0.113.9, 198.51.100.1", "198.51.100.1", 1},
		{"two hops", "198.51.100.1, 192.0.2.20", "198.51.100.1", 2},
		{"too short fails closed", "198.51.100.1", "192.0.2.10", 2},
		{"invalid fails closed", "bad", "192.0.2.10", 1},
		{"ipv6", "2001:db8::1", "2001:db8::1", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), "POST", "/", nil)
			r.RemoteAddr = "192.0.2.10:1234"
			r.Header.Set("X-Forwarded-For", tt.forwarded)
			s := &Service{limits: DefaultRateLimits()}
			s.limits.TrustedProxyHops = tt.hops
			if got := s.clientIP(r); got != tt.want {
				t.Fatalf("IP %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIndependentAuthenticationKeys(t *testing.T) {
	s, err := New(nil, strings.Repeat("test-key", 8), new(MailSink))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(s.key, s.otpKey) || bytes.Equal(s.key, s.oauthKey) || bytes.Equal(s.otpKey, s.oauthKey) || bytes.Equal(s.key, []byte(strings.Repeat("test-key", 8))) {
		t.Fatal("authentication purposes share a key")
	}
}

func TestGoogleClaimsAndFixtureProductionGuard(t *testing.T) {
	identity := ProviderIdentity{Provider: "google", Subject: "subject", Email: "learner@example.test", EmailVerified: true, Nonce: "expected", Issuer: "https://accounts.google.com", Audience: "client", ExpiresAt: time.Now().Add(time.Hour)}
	if err := ValidateGoogleIdentity(identity, "expected", "client"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"issuer", "audience", "nonce", "expiry"} {
		bad := identity
		switch field {
		case "issuer":
			bad.Issuer = "https://evil.test"
		case "audience":
			bad.Audience = "other"
		case "nonce":
			bad.Nonce = "other"
		case "expiry":
			bad.ExpiresAt = time.Now().Add(-time.Second)
		}
		if ValidateGoogleIdentity(bad, "expected", "client") == nil {
			t.Errorf("accepted wrong %s", field)
		}
	}
	for _, env := range []string{"", "production", "staging"} {
		t.Setenv("DUOLIFY_ENV", env)
		if _, err := NewWithOAuthProviders(nil, strings.Repeat("test-key", 8), new(MailSink), &FixtureGoogleProvider{}, &FixtureAppleProvider{}); err == nil {
			t.Fatalf("fixtures accepted in %q", env)
		}
	}
}

func TestProductionAuthDefaults(t *testing.T) {
	for _, env := range []string{"", "production", "staging"} {
		// #nosec G101 -- public development placeholder, intentionally rejected outside development.
		values := map[string]string{"DUOLIFY_ENV": env, "JWT_SECRET": "change-me-development-only-at-least-32-bytes"}
		if _, _, err := AuthFromEnv(func(k string) string { return values[k] }, nil); err == nil {
			t.Fatalf("unsafe config accepted in %q", env)
		}
		values["JWT_SECRET"] = strings.Repeat("a", 64)
		values["RESEND_API_KEY"] = "test-only"
		values["AUTH_MAIL_FROM"] = "login@example.test"
		if _, _, err := AuthFromEnv(func(k string) string { return values[k] }, nil); err == nil {
			t.Fatal("low entropy secret accepted")
		}
		values["JWT_SECRET"] = "test-only-production-key-with-varied-characters-123456789"
		if _, _, err := AuthFromEnv(func(k string) string { return values[k] }, nil); err != nil {
			t.Fatal(err)
		}
	}
}
