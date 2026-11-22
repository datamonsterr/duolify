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
// manifests, account and every other route stay private and uncacheable. CachePolicy further
// restricts the immutable value to successful (2xx/304) responses.
func CacheControlFor(method, path string) string {
	if (method == http.MethodGet || method == http.MethodHead) && immutableObject.MatchString(path) {
		return cacheImmutable
	}
	return cachePrivate
}

// cacheWriter downgrades the immutable default to private when the response is not a success,
// so an error served for a content address is never cached for a year.
type cacheWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *cacheWriter) WriteHeader(status int) {
	if !w.wrote {
		w.wrote = true
		h := w.Header()
		if h.Get("Cache-Control") == cacheImmutable && (status < 200 || status > 299) && status != http.StatusNotModified {
			h.Set("Cache-Control", cachePrivate)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *cacheWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *cacheWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// CachePolicy sets the default Cache-Control before the handler runs; handlers keep their own.
func CachePolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cc := CacheControlFor(r.Method, r.URL.Path)
		w.Header().Set("Cache-Control", cc)
		if cc == cacheImmutable {
			w = &cacheWriter{ResponseWriter: w}
		}
		next.ServeHTTP(w, r)
	})
}
