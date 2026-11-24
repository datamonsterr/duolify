//go:build integration

package accounts_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/accountstest"
)

func TestTurnstileAuthIntegration(t *testing.T) {
	h := newErasureHarness(t)
	h.auth.ConfigureRateLimits(accounts.RateLimits{RequestIP: 100, RequestEmail: 100, VerifyIP: 100})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(150 * time.Millisecond)
			return
		}
		accountstest.FakeSiteverify(w, r)
	}))
	t.Cleanup(fixture.Close)
	configure := func(secret, path, hostname string, timeout time.Duration) {
		client := &http.Client{Timeout: timeout}
		verifier := accounts.NewSiteverify(secret, fixture.URL+path, []string{hostname}, client)
		h.auth.ConfigureTurnstile(accounts.NewTurnstile(accountstest.TurnstileSiteKey, verifier, nil))
	}
	deny := func(t *testing.T, path string, body any) {
		t.Helper()
		if w := h.call(http.MethodPost, path, body, ""); w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"turnstile_failed"`) {
			t.Fatalf("challenge denial: %d %s", w.Code, w.Body)
		}
	}
	for _, tc := range []struct {
		name, secret, path, hostname, proof string
		timeout                             time.Duration
	}{
		{"missing", accountstest.TurnstilePassSecret, "", accountstest.TurnstileHostname, "", time.Second},
		{"invalid", accountstest.TurnstileFailSecret, "", accountstest.TurnstileHostname, "correct", time.Second},
		{"spent", accountstest.TurnstileSpentSecret, "", accountstest.TurnstileHostname, "correct", time.Second},
		{"timeout", accountstest.TurnstilePassSecret, "/slow", accountstest.TurnstileHostname, "correct", 15 * time.Millisecond},
		{"wrong action", accountstest.TurnstilePassSecret, "", accountstest.TurnstileHostname, "share_preview:wrong", time.Second},
		{"wrong hostname", accountstest.TurnstilePassSecret, "", "another.example.com", "correct", time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email := fmt.Sprintf("cf04-%s@example.test", strings.ReplaceAll(tc.name, " ", "-"))
			configure(tc.secret, tc.path, tc.hostname, tc.timeout)
			data := map[string]any{"email": email, "age_confirmed": true, "turnstile_token": tc.proof}
			if tc.proof == "correct" {
				data["turnstile_token"] = "email_code:denied"
			}
			deny(t, "/v1/auth/code", data)
			var codes int
			if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM auth_codes WHERE email=$1`, email).Scan(&codes); err != nil || codes != 0 || h.mail.Code(email) != "" {
				t.Fatalf("proof failure recorded or sent an OTP: %d, %v", codes, err)
			}
			configure(accountstest.TurnstilePassSecret, "", accountstest.TurnstileHostname, time.Second)
			data["turnstile_token"] = "email_code:fresh"
			if w := h.call(http.MethodPost, "/v1/auth/code", data, ""); w.Code != 202 || h.mail.Code(email) == "" {
				t.Fatalf("verified request did not send an OTP: %d %s", w.Code, w.Body)
			}
			data["code"] = h.mail.Code(email)
			data["turnstile_token"] = tc.proof
			if tc.proof == "correct" {
				data["turnstile_token"] = "sign_up:denied"
			}
			configure(tc.secret, tc.path, tc.hostname, tc.timeout)
			deny(t, "/v1/auth/verify", data)
			var consumed, registered bool
			var attempts int
			if err := h.pool.QueryRow(h.ctx, `SELECT consumed_at IS NOT NULL, attempts, EXISTS(SELECT 1 FROM auth_identities WHERE email=$1) FROM auth_codes WHERE email=$1`, email).Scan(&consumed, &attempts, &registered); err != nil || consumed || attempts != 0 || registered {
				t.Fatalf("proof failure touched the OTP or identity: consumed=%t, attempts=%d, registered=%t, err=%v", consumed, attempts, registered, err)
			}
			configure(accountstest.TurnstilePassSecret, "", accountstest.TurnstileHostname, time.Second)
			data["turnstile_token"] = "sign_up:fresh"
			if w := h.call(http.MethodPost, "/v1/auth/verify", data, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"access_token"`) {
				t.Fatalf("verified OTP did not create a session: %d %s", w.Code, w.Body)
			}
		})
	}
	// Challenge failures still spend the pre-existing request-IP and verification-IP budgets.
	h.auth.ConfigureRateLimits(accounts.RateLimits{RequestIP: 1, RequestEmail: 100, VerifyIP: 1})
	configure(accountstest.TurnstilePassSecret, "", accountstest.TurnstileHostname, time.Second)
	data := map[string]any{"email": "cf04-limited@example.test", "age_confirmed": true, "turnstile_token": "email_code:fresh"}
	if w := h.call(http.MethodPost, "/v1/auth/code", data, ""); w.Code != 429 {
		t.Fatalf("request budget after rejected proof: %d", w.Code)
	}
	data["turnstile_token"] = "sign_up:fresh"
	if w := h.call(http.MethodPost, "/v1/auth/verify", data, ""); w.Code != 429 {
		t.Fatalf("verification budget after rejected proof: %d", w.Code)
	}
}
