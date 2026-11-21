package telemetry

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// Route mounts an extra handler on the operations listener.
type Route struct {
	Pattern string
	Handler http.Handler
}

// Listen serves only private operational endpoints (plus any extra routes) and returns a shutdown hook.
func (m *Monitor) Listen(ctx context.Context, addr string, extra ...Route) (func(), error) {
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, errors.New("operational listener unavailable")
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m)
	for _, route := range extra {
		mux.Handle(route.Pattern, route.Handler)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.Incident(ctx, "dependency", "metrics", false)
		}
	}()
	return func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}, nil
}
