package accounts_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
)

func TestSessionMutationsRequireAuthenticationAndCORS(t *testing.T) {
	auth, err := accounts.New(nil, strings.Repeat("test-key-", 5), new(accounts.MailSink))
	if err != nil {
		t.Fatal(err)
	}
	router := httpapi.New(httpapi.Dependencies{Accounts: auth}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, method := range []string{"GET", "DELETE"} {
		for _, path := range []string{"/v1/auth/sessions", "/v1/auth/sessions/56d4e614-7665-4f14-b83e-8b5e9bed384a"} {
			if method == "GET" && strings.Count(path, "/") == 4 {
				continue
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), method, path, nil))
			if w.Code != 401 {
				t.Errorf("%s %s anonymous = %d", method, path, w.Code)
			}
		}
	}
	for _, origin := range []string{"tauri://localhost", "http://localhost:1420", "https://tauri.localhost"} {
		r := httptest.NewRequestWithContext(t.Context(), "OPTIONS", "/v1/auth/sessions", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Access-Control-Request-Method", "DELETE")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Fatal("preflight", w.Code, w.Header())
		}
		for _, method := range []string{"GET", "POST", "PATCH", "PUT", "DELETE"} {
			if !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), method) {
				t.Errorf("missing CORS %s", method)
			}
		}
	}
	r := httptest.NewRequestWithContext(t.Context(), "OPTIONS", "/v1/auth/sessions", nil)
	r.Header.Set("Origin", "https://evil.test")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("foreign origin", w.Code)
	}
}
