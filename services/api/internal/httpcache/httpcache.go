// Package httpcache writes JSON responses with an explicit Cache-Control and a strong ETag,
// answering a matching If-None-Match with 304. Public values are for non-personal responses only.
package httpcache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Cache-Control values. Public ones are never used for a response that depends on the caller.
const (
	// Public is for non-personal, slowly changing GETs: browsers and Cloudflare may reuse it for
	// a minute, and a revalidation is a 304.
	Public = "public, max-age=60"
	// Private is for per-learner reads: stored only by the learner's own browser and always
	// revalidated, so a change shows at once and an unchanged body costs a 304.
	Private = "private, no-cache"
)

// WriteJSON encodes v, sets Cache-Control and ETag and serves 304 when If-None-Match matches.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, cacheControl string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encoding failed", http.StatusInternalServerError)
		return
	}
	WriteBytes(w, r, status, cacheControl, append(body, '\n'))
}

// WriteBytes is WriteJSON for an already encoded body.
func WriteBytes(w http.ResponseWriter, r *http.Request, status int, cacheControl string, body []byte) {
	h := w.Header()
	h.Set("Cache-Control", cacheControl)
	if cacheControl == Private {
		h.Add("Vary", "Authorization")
	}
	h.Set("Content-Type", "application/json")
	if status == http.StatusOK {
		etag := fmt.Sprintf(`"%x"`, sha256.Sum256(body))
		h.Set("ETag", etag)
		if Matches(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// Matches reports whether If-None-Match names etag (weak comparison) or is "*".
func Matches(r *http.Request, etag string) bool {
	for _, tag := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" || strings.TrimPrefix(tag, "W/") == etag {
			return true
		}
	}
	return false
}
