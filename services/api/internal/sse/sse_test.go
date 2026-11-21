package sse

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntervalDefaultsAndOverrides(t *testing.T) {
	for env, want := range map[string]time.Duration{"": DefaultInterval, "bogus": DefaultInterval, "-1s": DefaultInterval, "0": DefaultInterval, "50ms": 50 * time.Millisecond} {
		t.Setenv("SSE_HEARTBEAT_INTERVAL", env)
		if got := Interval(); got != want {
			t.Errorf("%q: got %v want %v", env, got, want)
		}
	}
	if DefaultInterval >= 25*time.Second {
		t.Fatal("default must stay under the 25 s spec budget")
	}
}

func TestHeartbeatWritesCommentAndFlushes(t *testing.T) {
	w := httptest.NewRecorder()
	if err := Heartbeat(w); err != nil || w.Body.String() != ": keepalive\n\n" || !w.Flushed {
		t.Fatalf("%v %q flushed=%v", err, w.Body.String(), w.Flushed)
	}
}
