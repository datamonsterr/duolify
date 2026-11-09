// Package store owns PostgreSQL connections and generated sqlc queries.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store shares one pool between queries and the River queue.
type Store struct {
	Pool    *pgxpool.Pool
	Queries *db.Queries
}

// New constructs a pool; readiness performs the live connectivity check.
func New(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_URL")
	}
	cfg.ConnConfig.ConnectTimeout = 3 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	return &Store{Pool: pool, Queries: db.New(pool)}, nil
}

// Ping verifies a live database connection.
func (s *Store) Ping(ctx context.Context) error { return s.Pool.Ping(ctx) }

// Close drains and releases pooled connections.
func (s *Store) Close() { s.Pool.Close() }
