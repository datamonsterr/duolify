package store

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig sizes one process's pgx pool. Every process (each API replica, the worker, a one-shot
// command) has its own pool, so the sum of MaxConns over all of them, plus a replica during a rolling
// restart, must stay under Postgres max_connections (checked by infra:deploy-plan).
type PoolConfig struct {
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
}

// DefaultPoolConfig is sized for the 2 vCPU staging VM: pgx's own default is max(4, CPUs), too small for
// an API that also serves SSE pollers. The worker raises MaxConns by env because its River clients each
// hold a LISTEN connection.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConns: 10, MinConns: 2,
		MaxConnLifetime: 30 * time.Minute, MaxConnIdleTime: 5 * time.Minute,
		HealthCheckPeriod: time.Minute, ConnectTimeout: 3 * time.Second,
	}
}

// PoolConfigFromEnv reads the DB_POOL_* settings over DefaultPoolConfig. Unset or empty keeps the
// default; an invalid value is an error so a typo cannot silently leave the pool unsized.
func PoolConfigFromEnv(getenv func(string) string) (PoolConfig, error) {
	cfg := DefaultPoolConfig()
	for _, item := range []struct {
		name string
		dst  *int32
		min  int
	}{{"DB_POOL_MAX_CONNS", &cfg.MaxConns, 1}, {"DB_POOL_MIN_CONNS", &cfg.MinConns, 0}} {
		raw := getenv(item.name)
		if raw == "" {
			continue
		}
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n < int64(item.min) || n > 1000 {
			return PoolConfig{}, fmt.Errorf("%s must be an integer from %d to 1000", item.name, item.min)
		}
		*item.dst = int32(n)
	}
	for _, item := range []struct {
		name string
		dst  *time.Duration
	}{
		{"DB_POOL_MAX_CONN_LIFETIME", &cfg.MaxConnLifetime}, {"DB_POOL_MAX_CONN_IDLE_TIME", &cfg.MaxConnIdleTime},
		{"DB_POOL_HEALTH_CHECK_PERIOD", &cfg.HealthCheckPeriod}, {"DB_CONNECT_TIMEOUT", &cfg.ConnectTimeout},
	} {
		raw := getenv(item.name)
		if raw == "" {
			continue
		}
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return PoolConfig{}, fmt.Errorf("%s must be a positive duration such as 30m", item.name)
		}
		*item.dst = d
	}
	if cfg.MinConns > cfg.MaxConns {
		return PoolConfig{}, fmt.Errorf("DB_POOL_MIN_CONNS (%d) exceeds DB_POOL_MAX_CONNS (%d)", cfg.MinConns, cfg.MaxConns)
	}
	return cfg, nil
}

// apply sets the pool fields on a parsed pgx config. Parameters the DATABASE_URL names itself
// (pool_max_conns and friends) win over the defaults, but not over an explicit DB_POOL_* variable.
func (p PoolConfig) apply(cfg *pgxpool.Config, databaseURL string, getenv func(string) string) {
	query := url.Values{}
	if u, err := url.Parse(databaseURL); err == nil {
		query = u.Query()
	}
	fromURL := func(key, env string) bool { return query.Has(key) && getenv(env) == "" }
	if !fromURL("pool_max_conns", "DB_POOL_MAX_CONNS") {
		cfg.MaxConns = p.MaxConns
	}
	if !fromURL("pool_min_conns", "DB_POOL_MIN_CONNS") {
		cfg.MinConns = p.MinConns
	}
	if !fromURL("pool_max_conn_lifetime", "DB_POOL_MAX_CONN_LIFETIME") {
		cfg.MaxConnLifetime = p.MaxConnLifetime
	}
	if !fromURL("pool_max_conn_idle_time", "DB_POOL_MAX_CONN_IDLE_TIME") {
		cfg.MaxConnIdleTime = p.MaxConnIdleTime
	}
	if !fromURL("pool_health_check_period", "DB_POOL_HEALTH_CHECK_PERIOD") {
		cfg.HealthCheckPeriod = p.HealthCheckPeriod
	}
	cfg.ConnConfig.ConnectTimeout = p.ConnectTimeout
}
