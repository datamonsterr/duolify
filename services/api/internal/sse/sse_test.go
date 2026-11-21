package sse

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// A stream outlives the server's WriteTimeout as long as heartbeats keep flowing (found by the E18-02
// load baseline: the API cut every stream at 20 s).
func TestHeartbeatExtendsServerWriteDeadline(t *testing.T) {
	t.Setenv("SSE_HEARTBEAT_INTERVAL", "100ms")
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for range 10 {
			if err := Heartbeat(w); err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		_, _ = w.Write([]byte("event: done\n\n"))
	}))
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Start()
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "event: done") {
		t.Fatalf("stream cut by WriteTimeout, got %q", body)
	}
}
