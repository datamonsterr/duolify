package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

// DefaultDrainTimeout bounds how long in-flight requests (quoted previews, SSE tutor streams) may finish
// after SIGTERM. It exceeds the longest quoted preview/lesson budget; compose stop_grace_period must stay above it.
const DefaultDrainTimeout = 90 * time.Second

// DrainTimeoutFrom reads API_DRAIN_TIMEOUT (a Go duration); empty or invalid keeps the default.
func DrainTimeoutFrom(getenv func(string) string) time.Duration {
	if d, err := time.ParseDuration(getenv("API_DRAIN_TIMEOUT")); err == nil && d > 0 {
		return d
	}
	return DefaultDrainTimeout
}

// drainGate fails GET /readyz once a drain starts, so a probing proxy or balancer stops routing here.
type drainGate struct {
	next     http.Handler
	draining atomic.Bool
}

func (g *drainGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g.draining.Load() && r.Method == http.MethodGet && r.URL.Path == "/readyz" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"draining"}`))
		return
	}
	g.next.ServeHTTP(w, r)
}

// NewServer sets limits for headers, request bodies, responses and idle connections.
func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           &drainGate{next: handler},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// Serve drains in-flight requests when the caller receives SIGTERM or SIGINT.
func Serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	return ServeWithDrain(ctx, server, listener, DrainTimeoutFrom(os.Getenv))
}

// ServeWithDrain stops accepting connections when ctx ends, fails /readyz, lets in-flight requests finish
// for up to drain, then closes what is left. Request contexts are not tied to ctx, so a drained request
// (including its engine gRPC call) is never cancelled by the signal.
func ServeWithDrain(ctx context.Context, server *http.Server, listener net.Listener, drain time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		if g, ok := server.Handler.(*drainGate); ok {
			g.draining.Store(true)
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), drain)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			closeErr := server.Close()
			return errors.Join(err, closeErr)
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
