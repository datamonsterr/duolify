package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	immutableHeader = "public, max-age=31536000, immutable"
	privateHeader   = "private, no-store"
)

func TestCacheControlForOnlyMarksImmutableNonPersonalObjectsPublic(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, c := range []struct{ method, path, want string }{
		{http.MethodGet, "/v1/objects/sha256-" + digest, immutableHeader},
		{http.MethodHead, "/v1/objects/sha256-" + digest, immutableHeader},
		{http.MethodPost, "/v1/objects/sha256-" + digest, privateHeader},
		{http.MethodGet, "/v1/objects/sha256-" + digest[:63], privateHeader},
		{http.MethodGet, "/v1/objects/sha256-" + strings.ToUpper(digest), privateHeader},
		{http.MethodGet, "/v1/objects/sha256-" + digest + "/../me", privateHeader},
		{http.MethodGet, "/v1/lessons/l1/manifest", privateHeader},
		{http.MethodGet, "/v1/lessons/l1/slides/s1", privateHeader},
		{http.MethodGet, "/v1/lessons/l1/slides/s1/narration/audio", privateHeader},
		{http.MethodGet, "/v1/me", privateHeader},
		{http.MethodGet, "/v1/me/credits", privateHeader},
		{http.MethodGet, "/v1/courses/c1/download", privateHeader},
		{http.MethodGet, "/v1/courses/c1/tutor/messages", privateHeader},
		{http.MethodGet, "/", privateHeader},
	} {
		if got := CacheControlFor(c.method, c.path); got != c.want {
			t.Errorf("%s %s: got %q want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestCachePolicyMiddlewareDefaultsPrivateAndKeepsHandlerChoice(t *testing.T) {
	digest := strings.Repeat("b", 64)
	h := CachePolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/pricing" {
			w.Header().Set("Cache-Control", "public, max-age=60")
		}
		w.WriteHeader(http.StatusOK)
	}))
	for path, want := range map[string]string{
		"/v1/objects/sha256-" + digest: immutableHeader,
		"/v1/lessons/l1/manifest":      privateHeader,
		"/v1/pricing":                  "public, max-age=60",
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		if got := w.Header().Get("Cache-Control"); got != want {
			t.Errorf("%s: got %q want %q", path, got, want)
		}
	}
}

func TestCachePolicyImmutableOnlyForSuccessStatuses(t *testing.T) {
	path := "/v1/objects/sha256-" + strings.Repeat("c", 64)
	for status, want := range map[int]string{
		http.StatusOK:                  immutableHeader,
		http.StatusNotModified:         immutableHeader,
		http.StatusUnauthorized:        privateHeader,
		http.StatusNotFound:            privateHeader,
		http.StatusInternalServerError: privateHeader,
	} {
		h := CachePolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		if got := w.Header().Get("Cache-Control"); got != want {
			t.Errorf("status %d: got %q want %q", status, got, want)
		}
	}
}

func TestCachePolicyImmutableImplicitOKAndHandlerOverride(t *testing.T) {
	path := "/v1/objects/sha256-" + strings.Repeat("d", 64)
	h := CachePolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	if got := w.Header().Get("Cache-Control"); got != immutableHeader {
		t.Errorf("implicit 200: got %q", got)
	}
	h = CachePolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusNotFound)
	}))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("override: got %q", got)
	}
}
