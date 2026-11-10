package e2e

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

type countingAuthMailer struct {
	accounts.MailSink
	deliveries atomic.Int32
}

func (m *countingAuthMailer) SendCode(ctx context.Context, email, code string) error {
	m.deliveries.Add(1)
	return m.MailSink.SendCode(ctx, email, code)
}

func TestAuthHardeningConcurrentRefreshAndEmailIsolation(t *testing.T) {
	_, _, database := signedInDatabase(t)
	sink := new(countingAuthMailer)
	auth, err := accounts.New(database.Pool, "test-only-auth-hardening-key-at-least-32-bytes", sink)
	if err != nil {
		t.Fatal(err)
	}
	limits := accounts.DefaultRateLimits()
	limits.TrustedProxyHops = 1
	auth.ConfigureRateLimits(limits)
	call := func(path, body, client string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), "POST", path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("X-Forwarded-For", client)
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, r)
		return w
	}
	const body = `{"email":"victim@example.test","age_confirmed":true}`
	if w := call("/v1/auth/code", body, "198.51.100.1"); w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	code := sink.Code("victim@example.test")
	for i := range 4 {
		want := 202
		if i >= 2 {
			want = 429
		}
		if w := call("/v1/auth/code", body, "198.51.100.1"); w.Code != want {
			t.Fatal("per-caller request limit", w.Code, want)
		}
	}
	if w := call("/v1/auth/code", body, "198.51.100.2"); w.Code != 202 {
		t.Fatal("attacker locked out victim", w.Code, w.Body)
	}
	if sink.Code("victim@example.test") != code {
		t.Fatal("unexpired code overwritten")
	}
	if sink.deliveries.Load() != 1 {
		t.Fatal("resend cooldown did not suppress delivery")
	}
	w := call("/v1/auth/verify", `{"email":"victim@example.test","age_confirmed":true,"code":"`+code+`"}`, "198.51.100.2")
	if w.Code != 200 {
		t.Fatal("original code rejected", w.Code, w.Body)
	}
	var original accounts.Tokens
	if err := json.Unmarshal(w.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	results := make(chan accounts.Tokens, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			w := call("/v1/auth/refresh", `{"refresh_token":"`+original.RefreshToken+`"}`, "198.51.100.2")
			if w.Code != 200 {
				t.Error("refresh race logged out", w.Code, w.Body)
				return
			}
			var got accounts.Tokens
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Error(err)
				return
			}
			results <- got
		})
	}
	wg.Wait()
	close(results)
	var first accounts.Tokens
	for got := range results {
		if first.RefreshToken == "" {
			first = got
		}
		if got != first {
			t.Error("refresh race returned different successor")
		}
	}
	if _, err := database.Pool.Exec(t.Context(), "UPDATE auth_refresh_tokens SET consumed_at=now()-interval '1 minute' WHERE token_hash IS NOT NULL AND consumed_at IS NOT NULL"); err != nil {
		t.Fatal(err)
	}
	if w := call("/v1/auth/refresh", `{"refresh_token":"`+original.RefreshToken+`"}`, "198.51.100.2"); w.Code != 401 {
		t.Fatal("late reuse accepted", w.Code)
	}
	if w := call("/v1/auth/refresh", `{"refresh_token":"`+first.RefreshToken+`"}`, "198.51.100.2"); w.Code != 401 {
		t.Fatal("late reuse did not revoke family", w.Code)
	}
	limits.DeliveryEmail = 1
	auth.ConfigureRateLimits(limits)
	const capped = `{"email":"delivery-cap@example.test","age_confirmed":true}`
	if w := call("/v1/auth/code", capped, "198.51.100.3"); w.Code != 202 {
		t.Fatal("first delivery", w.Code)
	}
	cappedCode := sink.Code("delivery-cap@example.test")
	if _, err := database.Pool.Exec(t.Context(), "UPDATE auth_codes SET last_sent_at=now()-interval '61 seconds' WHERE email='delivery-cap@example.test'"); err != nil {
		t.Fatal(err)
	}
	if w := call("/v1/auth/code", capped, "198.51.100.4"); w.Code != 202 {
		t.Fatal("delivery cap must be opaque", w.Code)
	}
	if sink.deliveries.Load() != 2 || sink.Code("delivery-cap@example.test") != cappedCode {
		t.Fatal("email delivery cap bypassed or active code replaced")
	}
	if w := call("/v1/auth/verify", `{"email":"delivery-cap@example.test","age_confirmed":true,"code":"`+cappedCode+`"}`, "198.51.100.4"); w.Code != 200 {
		t.Fatal("delivery cap blocked verification", w.Code)
	}
}
