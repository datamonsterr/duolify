// pricingserver is an isolated Playwright fixture: empty Postgres, real migrations,
// real pricing handler, no provider calls, no production database or credentials.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (result error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	boot, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(boot, "postgres:18", postgres.WithDatabase("pricing_e2e"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result = errors.Join(result, container.Terminate(cleanup))
	}()
	url, err := container.ConnectionString(boot, "sslmode=disable")
	if err != nil {
		return err
	}
	if err := migrations.Up(boot, url); err != nil {
		return err
	}
	db, err := store.New(boot, url)
	if err != nil {
		return err
	}
	defer db.Close()
	catalogue, err := pricing.Load(boot, db.Queries)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httpapi.NewServer("127.0.0.1:18081", httpapi.New(httpapi.Dependencies{Pricing: catalogue}, logger))
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", server.Addr)
	if err != nil {
		return err
	}
	return httpapi.Serve(ctx, server, listener)
}
