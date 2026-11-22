package accounts_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

const webOrigin = "https://app.example.test"

func webService(t *testing.T) *accounts.Service {
	t.Helper()
	auth, err := accounts.New(nil, strings.Repeat("test-key-", 5), new(accounts.MailSink))
	if err != nil {
		t.Fatal(err)
	}
	auth.ConfigureWebOrigins([]string{" " + webOrigin + " ", ""})
	return auth
}

// Cross-site requests are refused before any state is read or changed, so a nil pool proves
// no database work happens for them.
func TestWebSessionRejectsCrossSiteRequests(t *testing.T) {
	auth := webService(t)
	for _, path := range []string{"/v1/auth/refresh", "/v1/auth/logout", "/v1/auth/verify"} {
		for name, headers := range map[string]map[string]string{
			"foreign origin":      {"Origin": "https://evil.example.test"},
			"missing origin":      {},
			"origin prefix":       {"Origin": webOrigin + ".evil.test"},
			"sec-fetch cross":     {"Origin": webOrigin, "Sec-Fetch-Site": "cross-site"},
			"sec-fetch same-site": {"Origin": webOrigin, "Sec-Fetch-Site": "same-site"},
			"sec-fetch none":      {"Origin": webOrigin, "Sec-Fetch-Site": "none"},
		} {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set(accounts.ClientHeader, accounts.ClientWeb)
			req.Header.Add("Cookie", accounts.RefreshCookieName+"=stolen")
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			auth.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden || len(w.Header().Values("Set-Cookie")) != 0 {
				t.Errorf("%s %s: %d %v", path, name, w.Code, w.Header())
			}
		}
	}
}

func TestWebSessionNoConfiguredOriginRefusesCookieMode(t *testing.T) {
	auth, err := accounts.New(nil, strings.Repeat("test-key-", 5), new(accounts.MailSink))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/refresh", nil)
	req.Header.Set(accounts.ClientHeader, accounts.ClientWeb)
	req.Header.Set("Origin", webOrigin)
	w := httptest.NewRecorder()
	auth.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatal(w.Code)
	}
}

func TestWebSessionRefreshAndLogoutAreNotReadable(t *testing.T) {
	auth := webService(t)
	for _, path := range []string{"/v1/auth/refresh", "/v1/auth/logout"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		req.Header.Set(accounts.ClientHeader, accounts.ClientWeb)
		req.Header.Set("Origin", webOrigin)
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, req)
		if w.Code != http.StatusMethodNotAllowed || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Errorf("GET %s changed state: %d", path, w.Code)
		}
	}
}

func TestWebOriginsFromEnv(t *testing.T) {
	got := accounts.WebOriginsFromEnv(func(string) string { return webOrigin + ", http://localhost" })
	if len(got) != 2 {
		t.Fatal(got)
	}
}

// With no cookie the body credential is ignored: 401 and the cookie is cleared, without
// touching the database.
func TestWebSessionRefreshWithoutCookie(t *testing.T) {
	auth := webService(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/refresh", strings.NewReader(`{"refresh_token":"body-token-must-be-ignored"}`))
	req.Header.Set(accounts.ClientHeader, accounts.ClientWeb)
	req.Header.Set("Origin", webOrigin)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	auth.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code, w.Body)
	}
	if c := w.Header().Get("Set-Cookie"); !strings.Contains(c, "Max-Age=0") || !strings.Contains(c, "Path=/;") {
		t.Fatal("cookie not cleared:", c)
	}
}
