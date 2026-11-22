package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/characters"
)

// bearerAuth rejects a request without a bearer token, like the real session guard, and serves a
// per-learner body otherwise.
type bearerAuth struct{}

func (bearerAuth) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, `{"learner":"someone"}`)
}
func (bearerAuth) Protect(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "sign in", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func policyRouter(t *testing.T) http.Handler {
	t.Helper()
	return New(Dependencies{
		Accounts:   bearerAuth{},
		Characters: characters.NewHandler(nil),
		Gamification: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"xp":1}`)
		}),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func get(t *testing.T, h http.Handler, path, token, etag string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// The table of public, non-personal GETs and their policies; see the PR body for the reasoning.
func TestPublicNonPersonalGETsHaveExplicitPublicPolicy(t *testing.T) {
	h := policyRouter(t)
	for path, want := range map[string]string{
		"/v1/features":                "public, max-age=60",
		"/v1/characters/default-cast": "public, max-age=60",
		"/v1/courses/shared-demo":     "public, max-age=300",
		"/healthz":                    privateHeader,
		"/v1/marketplace/courses":     privateHeader,
		"/v1/public/shares/abc":       privateHeader,
	} {
		w := get(t, h, path, "", "")
		if got := w.Header().Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control %q, want %q (status %d)", path, got, want, w.Code)
		}
	}
}

func TestPublicGETsRevalidateWithETag(t *testing.T) {
	h := policyRouter(t)
	for _, path := range []string{"/v1/features", "/v1/characters/default-cast", "/v1/courses/shared-demo"} {
		first := get(t, h, path, "", "")
		etag := first.Header().Get("ETag")
		if first.Code != http.StatusOK || etag == "" {
			t.Fatalf("%s: status=%d etag=%q", path, first.Code, etag)
		}
		if again := get(t, h, path, "", etag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
			t.Errorf("%s: If-None-Match gave %d with %d body bytes", path, again.Code, again.Body.Len())
		}
		if other := get(t, h, path, "", `"nope"`); other.Code != http.StatusOK {
			t.Errorf("%s: stale validator gave %d", path, other.Code)
		}
	}
}

// Whatever the route, an authenticated or per-learner response is never publicly cacheable.
func TestAuthenticatedResponsesAreNeverPublic(t *testing.T) {
	h := policyRouter(t)
	for _, path := range []string{
		"/v1/me", "/v1/me/credits", "/v1/gamification", "/v1/courses", "/v1/courses/c1",
		"/v1/lessons/l1/manifest", "/v1/notes", "/v1/social/friends", "/v1/credits/wallet",
	} {
		for _, token := range []string{"", "learner-a"} {
			w := get(t, h, path, token, "")
			cc := w.Header().Get("Cache-Control")
			if strings.Contains(cc, "public") || (cc != privateHeader && !strings.HasPrefix(cc, "private")) {
				t.Errorf("%s (token %q, status %d): Cache-Control %q", path, token, w.Code, cc)
			}
		}
	}
}

func TestPricingKeepsItsOwnPublicPolicy(t *testing.T) {
	// The catalogue handler owns the pricing policy (snapshot TTL bound); the router must not
	// override it with the private default.
	h := New(Dependencies{Pricing: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=10")
	})}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if got := get(t, h, "/v1/pricing", "", "").Header().Get("Cache-Control"); got != "public, max-age=10" {
		t.Fatalf("got %q", got)
	}
}
