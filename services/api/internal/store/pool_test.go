package store

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestPoolConfigDefaultsAndOverrides(t *testing.T) {
	got, err := PoolConfigFromEnv(env(nil))
	if err != nil || got != DefaultPoolConfig() {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	if got.MaxConns != 10 || got.MinConns != 2 || got.MaxConnLifetime != 30*time.Minute || got.MaxConnIdleTime != 5*time.Minute || got.HealthCheckPeriod != time.Minute || got.ConnectTimeout != 3*time.Second {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	got, err = PoolConfigFromEnv(env(map[string]string{
		"DB_POOL_MAX_CONNS": "16", "DB_POOL_MIN_CONNS": "4", "DB_POOL_MAX_CONN_LIFETIME": "1h",
		"DB_POOL_MAX_CONN_IDLE_TIME": "90s", "DB_POOL_HEALTH_CHECK_PERIOD": "30s", "DB_CONNECT_TIMEOUT": "5s",
	}))
	want := PoolConfig{MaxConns: 16, MinConns: 4, MaxConnLifetime: time.Hour, MaxConnIdleTime: 90 * time.Second, HealthCheckPeriod: 30 * time.Second, ConnectTimeout: 5 * time.Second}
	if err != nil || got != want {
		t.Fatalf("overrides = %+v, %v; want %+v", got, err, want)
	}
	// Compose forwards unset variables as empty strings: that keeps the default.
	got, err = PoolConfigFromEnv(env(map[string]string{"DB_POOL_MAX_CONNS": "", "DB_POOL_MAX_CONN_LIFETIME": ""}))
	if err != nil || got != DefaultPoolConfig() {
		t.Fatalf("empty values = %+v, %v", got, err)
	}
}

func TestPoolConfigRejectsInvalidValues(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"DB_POOL_MAX_CONNS":           {"DB_POOL_MAX_CONNS": "0"},
		"DB_POOL_MAX_CONNS ":          {"DB_POOL_MAX_CONNS": "many"},
		"DB_POOL_MAX_CONNS  ":         {"DB_POOL_MAX_CONNS": "100000"},
		"DB_POOL_MIN_CONNS":           {"DB_POOL_MIN_CONNS": "-1"},
		"DB_POOL_MAX_CONN_LIFETIME":   {"DB_POOL_MAX_CONN_LIFETIME": "30"},
		"DB_POOL_MAX_CONN_IDLE_TIME":  {"DB_POOL_MAX_CONN_IDLE_TIME": "-5m"},
		"DB_POOL_HEALTH_CHECK_PERIOD": {"DB_POOL_HEALTH_CHECK_PERIOD": "0s"},
		"DB_CONNECT_TIMEOUT":          {"DB_CONNECT_TIMEOUT": "soon"},
		"DB_POOL_MIN_CONNS (":         {"DB_POOL_MIN_CONNS": "20", "DB_POOL_MAX_CONNS": "10"},
	} {
		if _, err := PoolConfigFromEnv(env(values)); err == nil || !strings.Contains(err.Error(), strings.Fields(name)[0]) {
			t.Errorf("%v: err = %v, want a message naming %s", values, err, strings.Fields(name)[0])
		}
	}
}

func TestPoolConfigApplyKeepsURLParametersUnlessEnvSet(t *testing.T) {
	const base = "postgres://u:p@postgres:5432/duolify?sslmode=disable"
	parse := func(url string) *pgxpool.Config {
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	// No URL parameter: the process defaults replace pgx's own (max(4, CPUs)).
	cfg := parse(base)
	DefaultPoolConfig().apply(cfg, base, env(nil))
	if cfg.MaxConns != 10 || cfg.MinConns != 2 || cfg.MaxConnIdleTime != 5*time.Minute || cfg.ConnConfig.ConnectTimeout != 3*time.Second {
		t.Fatalf("defaults not applied: max=%d min=%d idle=%s connect=%s", cfg.MaxConns, cfg.MinConns, cfg.MaxConnIdleTime, cfg.ConnConfig.ConnectTimeout)
	}
	// A pool parameter in DATABASE_URL wins over the default...
	withURL := base + "&pool_max_conns=7&pool_min_conns=1"
	cfg = parse(withURL)
	DefaultPoolConfig().apply(cfg, withURL, env(nil))
	if cfg.MaxConns != 7 || cfg.MinConns != 1 {
		t.Fatalf("URL parameters overridden: max=%d min=%d", cfg.MaxConns, cfg.MinConns)
	}
	// ...but not over an explicit DB_POOL_* variable.
	explicit := env(map[string]string{"DB_POOL_MAX_CONNS": "16"})
	fromEnv, err := PoolConfigFromEnv(explicit)
	if err != nil {
		t.Fatal(err)
	}
	cfg = parse(withURL)
	fromEnv.apply(cfg, withURL, explicit)
	if cfg.MaxConns != 16 || cfg.MinConns != 1 {
		t.Fatalf("env did not win for max only: max=%d min=%d", cfg.MaxConns, cfg.MinConns)
	}
}
