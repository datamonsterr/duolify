package httpapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// SIGTERM (ctx cancel) during a slow in-flight request: it completes, /readyz flips to 503, new connections are refused.
func TestServeWithDrainFinishesInFlightAndRejectsNew(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "done")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- ServeWithDrain(ctx, NewServer(addr, mux), listener, 5*time.Second) }()

	type result struct {
		body string
		err  error
	}
	slow := make(chan result, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/slow", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slow <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		slow <- result{string(b), err}
	}()
	<-started
	stop() // SIGTERM

	// Draining: the listener closes, so a new connection is refused.
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(context.Background(), "tcp", addr)
		if err != nil {
			break
		}
		_ = c.Close()
		if time.Now().After(deadline) {
			t.Fatal("new connections still accepted during drain")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-served:
		t.Fatalf("server exited with a request in flight: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if r := <-slow; r.err != nil || r.body != "done" {
		t.Fatalf("in-flight request: body=%q err=%v", r.body, r.err)
	}
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestDrainGateFailsReadinessOnly(t *testing.T) {
	g := &drainGate{next: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	get := func(path string) int {
		rec := &statusRecorder{header: http.Header{}}
		g.ServeHTTP(rec, mustRequest(t, path))
		return rec.code
	}
	if got := get("/readyz"); got != http.StatusNoContent {
		t.Fatalf("before drain: %d", got)
	}
	g.draining.Store(true)
	if got := get("/readyz"); got != http.StatusServiceUnavailable {
		t.Fatalf("draining /readyz: %d", got)
	}
	if got := get("/v1/x"); got != http.StatusNoContent {
		t.Fatalf("draining other path must still serve: %d", got)
	}
}

func TestDrainTimeoutFrom(t *testing.T) {
	for in, want := range map[string]time.Duration{"": DefaultDrainTimeout, "bad": DefaultDrainTimeout, "-1s": DefaultDrainTimeout, "45s": 45 * time.Second} {
		if got := DrainTimeoutFrom(func(string) string { return in }); got != want {
			t.Fatalf("%q: %v want %v", in, got, want)
		}
	}
}

type statusRecorder struct {
	header http.Header
	code   int
}

func (r *statusRecorder) Header() http.Header         { return r.header }
func (r *statusRecorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *statusRecorder) WriteHeader(c int)           { r.code = c }

func mustRequest(t *testing.T, path string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://x"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}
