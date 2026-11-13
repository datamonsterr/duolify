package telemetry

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// Listen serves only private operational endpoints and returns a shutdown hook.
func (m *Monitor) Listen(ctx context.Context, addr string) (func(), error) {
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, errors.New("operational listener unavailable")
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m)
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
