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

// Extend moves the connection's write deadline out by three intervals. The server-wide
// WriteTimeout (20 s) would otherwise cut every stream, while a stalled client still times out
// because the deadline only ever moves a bounded step past now. Call it before each write to a
// stream, including the first. ErrNotSupported (test recorders, wrapped writers without Unwrap)
// means there is no deadline to extend; other errors mean the connection is unusable and the next
// write fails and ends the stream, so they are deliberately not handled here.
func Extend(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(3 * Interval()))
}

// Heartbeat writes a comment line, which clients ignore, and flushes it to the proxy. Each beat
// also extends the write deadline (see Extend).
func Heartbeat(w http.ResponseWriter) error {
	Extend(w)
	rc := http.NewResponseController(w)
	if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
		return err
	}
	return rc.Flush()
}
