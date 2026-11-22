package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var wantSecurityHeaders = map[string]string{
	"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
	"X-Content-Type-Options":       "nosniff",
	"Referrer-Policy":              "no-referrer",
	"Content-Security-Policy":      "default-src 'none'; frame-ancestors 'none'",
	"Cross-Origin-Resource-Policy": "same-site",
}

func checkSecurityHeaders(t *testing.T, label string, h http.Header) {
	t.Helper()
	for name, want := range wantSecurityHeaders {
		if got := h.Values(name); len(got) != 1 || got[0] != want {
			t.Errorf("%s: %s = %q, want exactly %q", label, name, got, want)
		}
	}
	if strings.Contains(h.Get("Strict-Transport-Security"), "preload") {
		t.Errorf("%s: HSTS must not preload", label)
	}
}

// Every response carries the headers: success, CORS preflight, rejected origin, panic and unknown route.
func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	t.Setenv("DUOLIFY_ENV", "production")
	t.Setenv("API_ALLOWED_ORIGINS", "https://learn.example.com")
	router := New(Dependencies{}, slog.Default())
	for _, tt := range []struct {
		name, method, path, origin string
		want                       int
	}{
		{"health", http.MethodGet, "/healthz", "", 200},
		{"same-origin web call", http.MethodGet, "/healthz", "https://learn.example.com", 200},
		{"preflight", http.MethodOptions, "/v1/auth/refresh", "https://learn.example.com", 204},
		{"rejected origin", http.MethodGet, "/healthz", "https://evil.example", 403},
		{"unauthenticated route", http.MethodGet, "/v1/future-private-route", "", 401},
		{"unknown public path", http.MethodGet, "/nope", "", 401},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status %d, want %d", w.Code, tt.want)
			}
			checkSecurityHeaders(t, tt.name, w.Header())
		})
	}
}

// The browser must still see CORS and the refresh cookie on a credentialed web call: the security headers
// add to the response, they never replace what the handlers set.
func TestSecurityHeadersKeepCORSAndCookies(t *testing.T) {
	t.Setenv("API_ALLOWED_ORIGINS", "https://learn.example.com")
	handler := Middleware(slog.Default(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "__Host-duolify_refresh", Value: "v", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/refresh", nil)
	r.Header.Set("Origin", "https://learn.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	checkSecurityHeaders(t, "refresh", w.Header())
	if w.Header().Get("Access-Control-Allow-Origin") != "https://learn.example.com" {
		t.Error("CORS origin missing")
	}
	if c := w.Result().Cookies(); len(c) != 1 || c[0].Name != "__Host-duolify_refresh" {
		t.Errorf("cookie lost: %v", c)
	}
}

// A handler that sets its own value for a security header must not produce a duplicate.
func TestSecurityHeadersNotDuplicatedWhenHandlerSetsOne(t *testing.T) {
	handler := Middleware(slog.Default(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))
	checkSecurityHeaders(t, "handler override", w.Header())
}
