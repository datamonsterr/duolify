// Package sse holds the keepalive shared by every Server-Sent Events route.
package sse

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// DefaultInterval stays well under the 25 s spec budget and Cloudflare's 125 s proxy read timeout.
const DefaultInterval = 15 * time.Second

// Interval reads SSE_HEARTBEAT_INTERVAL (a Go duration) so tests can run fast; invalid or
// non-positive values fall back to DefaultInterval.
func Interval() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("SSE_HEARTBEAT_INTERVAL")); err == nil && d > 0 {
		return d
	}
	return DefaultInterval
}

// Heartbeat writes a comment line, which clients ignore, and flushes it to the proxy.
func Heartbeat(w http.ResponseWriter) error {
	if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
		return err
	}
	return http.NewResponseController(w).Flush()
}
