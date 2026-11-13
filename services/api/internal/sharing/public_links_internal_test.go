package sharing

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLinkBodyUsesConfiguredDomainAndScheme(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PublicDomain, cfg.AppScheme = "go.example.test", "duo"
	body := (&Service{cfg: cfg}).linkBody("tok_123")
	if body["preview_url"] != "https://go.example.test/c/tok_123" || body["deep_link"] != "duo://share/tok_123" || body["token"] != "tok_123" {
		t.Fatalf("link body: %v", body)
	}
}

func TestClientIPTrustsOnlyConfiguredProxyHops(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.10:5000"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if got := (&Service{}).clientIP(r); got != "192.0.2.10" {
		t.Errorf("untrusted proxy chain must be ignored, got %s", got)
	}
	if got := (&Service{cfg: Config{TrustedProxyHops: 1}}).clientIP(r); got != "203.0.113.9" {
		t.Errorf("one trusted hop: got %s", got)
	}
	r.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := (&Service{cfg: Config{TrustedProxyHops: 1}}).clientIP(r); got != "192.0.2.10" {
		t.Errorf("garbage header falls back to the peer, got %s", got)
	}
}

func TestLinkStateNeverCarriesTokenMaterial(t *testing.T) {
	raw, _ := json.Marshal(LinkState{Visibility: "link", Active: true, Version: 2})
	for _, banned := range []string{"token", "hash", "url"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("link state exposes %q: %s", banned, raw)
		}
	}
}

func TestTrustedProxyHopsComesFromAuthConfig(t *testing.T) {
	get := func(k string) string { return map[string]string{"AUTH_TRUSTED_PROXY_HOPS": "2"}[k] }
	if got := ConfigFromEnv(get).TrustedProxyHops; got != 2 {
		t.Errorf("hops: %d", got)
	}
	if got := ConfigFromEnv(func(string) string { return "99" }).TrustedProxyHops; got != 0 {
		t.Errorf("out of range hops must fall back to 0, got %d", got)
	}
}
