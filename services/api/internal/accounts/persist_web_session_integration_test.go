//go:build integration

package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPersistWebSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("websession"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	sink := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key-", 5), sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("API_ALLOWED_ORIGINS", webOrigin) // the CORS layer and cookie mode trust the same list
	auth.ConfigureWebOrigins(accounts.WebOriginsFromEnv(os.Getenv))
	handler := httpapi.New(httpapi.Dependencies{Accounts: auth}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	type opts struct {
		web    bool
		cookie string
		bearer string
		origin string
		site   string
	}
	call := func(method, path string, body any, o opts) *httptest.ResponseRecorder {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
		req := httptest.NewRequestWithContext(ctx, method, path, reader)
		if o.bearer != "" {
			req.Header.Set("Authorization", "Bearer "+o.bearer)
		}
		if o.web {
			req.Header.Set(accounts.ClientHeader, accounts.ClientWeb)
			req.Header.Set("Origin", webOrigin)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		if o.origin != "" {
			req.Header.Set("Origin", o.origin)
		}
		if o.site != "" {
			req.Header.Set("Sec-Fetch-Site", o.site)
		}
		if o.cookie != "" {
			req.AddCookie(&http.Cookie{Name: accounts.RefreshCookieName, Value: o.cookie})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	cookieOf := func(w *httptest.ResponseRecorder) *http.Cookie {
		t.Helper()
		for _, c := range w.Result().Cookies() {
			if c.Name == accounts.RefreshCookieName {
				return c
			}
		}
		t.Fatalf("no refresh cookie: %v", w.Header())
		return nil
	}
	login := func(email string, web bool) *httptest.ResponseRecorder {
		t.Helper()
		in := map[string]any{"email": email, "age_confirmed": true}
		if w := call("POST", "/v1/auth/code", in, opts{web: web}); w.Code != 202 {
			t.Fatal(w.Code, w.Body)
		}
		in["code"] = sink.Code(email)
		w := call("POST", "/v1/auth/verify", in, opts{web: web})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		return w
	}
	access := func(w *httptest.ResponseRecorder) string {
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body["access_token"].(string)
	}

	t.Run("cookie attributes and body", func(t *testing.T) {
		w := login("web@example.test", true)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if _, leaked := body["refresh_token"]; leaked || body["access_token"] == "" || strings.Contains(w.Body.String(), "refresh") {
			t.Fatal("web body must carry only the access token:", w.Body)
		}
		c := cookieOf(w)
		raw := w.Header().Get("Set-Cookie")
		if c.Value == "" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/v1/auth/" || c.Domain != "" {
			t.Fatal("cookie attributes:", raw)
		}
		if c.MaxAge < 29*24*3600 || c.MaxAge > 30*24*3600 {
			t.Fatal("Max-Age should match the 30 day session:", c.MaxAge)
		}
		if strings.Contains(raw, "Domain") || !strings.Contains(raw, "SameSite=Strict") || !strings.Contains(raw, "HttpOnly") || !strings.Contains(raw, "Secure") {
			t.Fatal(raw)
		}
	})

	t.Run("refresh via cookie rotates and reuse revokes the family", func(t *testing.T) {
		first := login("rotate@example.test", true)
		old := cookieOf(first).Value
		w := call("POST", "/v1/auth/refresh", nil, opts{web: true, cookie: old})
		if w.Code != 200 || strings.Contains(w.Body.String(), "refresh_token") {
			t.Fatal(w.Code, w.Body)
		}
		next := cookieOf(w)
		if next.Value == "" || next.Value == old || !next.HttpOnly || !next.Secure || next.SameSite != http.SameSiteStrictMode {
			t.Fatal("not rotated:", w.Header())
		}
		if r := call("GET", "/v1/me", nil, opts{bearer: access(w)}); r.Code != 200 {
			t.Fatal("rotated access rejected", r.Code)
		}
		if _, err := db.Pool.Exec(ctx, "UPDATE auth_refresh_tokens SET consumed_at=now()-interval '1 minute' WHERE consumed_at IS NOT NULL"); err != nil {
			t.Fatal(err)
		}
		replay := call("POST", "/v1/auth/refresh", nil, opts{web: true, cookie: old})
		if replay.Code != 401 || cookieOf(replay).MaxAge >= 0 {
			t.Fatal("reuse accepted or cookie kept:", replay.Code, replay.Header())
		}
		if r := call("GET", "/v1/me", nil, opts{bearer: access(w)}); r.Code != 401 {
			t.Fatal("family survived reuse", r.Code)
		}
		if r := call("POST", "/v1/auth/refresh", nil, opts{web: true, cookie: next.Value}); r.Code != 401 {
			t.Fatal("successor survived reuse", r.Code)
		}
	})

	t.Run("cross-site requests are rejected without state change", func(t *testing.T) {
		cookie := cookieOf(login("csrf@example.test", true)).Value
		for name, o := range map[string]opts{
			"foreign origin": {web: true, cookie: cookie, origin: "https://evil.example.test"},
			"cross-site":     {web: true, cookie: cookie, site: "cross-site"},
			"same-site":      {web: true, cookie: cookie, site: "same-site"},
		} {
			for _, path := range []string{"/v1/auth/refresh", "/v1/auth/logout"} {
				if w := call("POST", path, nil, o); w.Code != 403 {
					t.Fatalf("%s %s: %d", name, path, w.Code)
				}
			}
		}
		if w := call("GET", "/v1/auth/refresh", nil, opts{web: true, cookie: cookie}); w.Code < 400 || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("GET is not rejected", w.Code)
		}
		if w := call("POST", "/v1/auth/refresh", nil, opts{web: true, cookie: cookie}); w.Code != 200 {
			t.Fatal("rejected requests burned the session", w.Code)
		}
	})

	t.Run("native flow is unchanged and modes cannot downgrade each other", func(t *testing.T) {
		w := login("native@example.test", false)
		if len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("native client received a cookie")
		}
		var tokens accounts.Tokens
		if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil || tokens.RefreshToken == "" {
			t.Fatal("native body lost its refresh token", w.Body)
		}
		// A cookie never stands in for a body token in native mode...
		if r := call("POST", "/v1/auth/refresh", map[string]string{}, opts{cookie: tokens.RefreshToken}); r.Code == 200 {
			t.Fatal("native refresh accepted a cookie")
		}
		// ...and a body token never stands in for the cookie in web mode.
		if r := call("POST", "/v1/auth/refresh", map[string]string{"refresh_token": tokens.RefreshToken}, opts{web: true}); r.Code != 401 {
			t.Fatal("web refresh accepted a body token", r.Code)
		}
		r := call("POST", "/v1/auth/refresh", map[string]string{"refresh_token": tokens.RefreshToken}, opts{})
		if r.Code != 200 || len(r.Header().Values("Set-Cookie")) != 0 || !strings.Contains(r.Body.String(), "refresh_token") {
			t.Fatal("native refresh changed", r.Code, r.Header())
		}
		var rotated accounts.Tokens
		if err := json.Unmarshal(r.Body.Bytes(), &rotated); err != nil {
			t.Fatal(err)
		}
		if r := call("POST", "/v1/auth/logout", map[string]string{"refresh_token": rotated.RefreshToken}, opts{}); r.Code != 204 || len(r.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("native logout", r.Code, r.Header())
		}
	})

	t.Run("logout, sign out everywhere and delete clear the cookie", func(t *testing.T) {
		in := login("logout@example.test", true)
		cookie, bearer := cookieOf(in).Value, access(in)
		w := call("POST", "/v1/auth/logout", nil, opts{web: true, cookie: cookie})
		if w.Code != 204 || cookieOf(w).MaxAge >= 0 || cookieOf(w).Path != "/v1/auth/" {
			t.Fatal("logout", w.Code, w.Header())
		}
		if r := call("GET", "/v1/me", nil, opts{bearer: bearer}); r.Code != 401 {
			t.Fatal("logout left the session live", r.Code)
		}
		if r := call("POST", "/v1/auth/refresh", nil, opts{web: true, cookie: cookie}); r.Code != 401 {
			t.Fatal("logout left the cookie usable", r.Code)
		}
		all := login("everywhere@example.test", true)
		if w := call("DELETE", "/v1/auth/sessions", nil, opts{web: true, bearer: access(all)}); w.Code != 204 || cookieOf(w).MaxAge >= 0 {
			t.Fatal("sign out everywhere", w.Code, w.Header())
		}
	})
}
