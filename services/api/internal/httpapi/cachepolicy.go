package httpapi

import (
	"net/http"
	"regexp"
)

const (
	cacheImmutable = "public, max-age=31536000, immutable"
	cachePrivate   = "private, no-store"
)

// immutableObject matches content-addressed, learner-independent objects. Slides and narration
// are authorised per learner and keyed by lesson, so they never match.
var immutableObject = regexp.MustCompile(`^/v1/objects/sha256-[0-9a-f]{64}$`)

// CacheControlFor returns the edge cache policy: only GET/HEAD of an immutable object is public;
// manifests, account and every other route stay private and uncacheable.
func CacheControlFor(method, path string) string {
	if (method == http.MethodGet || method == http.MethodHead) && immutableObject.MatchString(path) {
		return cacheImmutable
	}
	return cachePrivate
}

// CachePolicy sets the default Cache-Control before the handler runs; handlers keep their own.
func CachePolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", CacheControlFor(r.Method, r.URL.Path))
		next.ServeHTTP(w, r)
	})
}
