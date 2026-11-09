// Package config validates runtime environment variables before opening dependencies.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Config contains service settings. Secrets must never be logged.
type Config struct {
	Addr                    string
	DatabaseURL             string
	RedisURL                string
	EngineAddr              string
	PostHogKey              string
	PostHogHost             string
	LogLevel                slog.Level
	AnalyticsOptOut         bool
	LessonTestTracerEnabled bool
}

// Load reads a supplied environment, allowing tests without process-global mutations.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:        defaultValue(getenv("API_ADDR"), ":8080"),
		DatabaseURL: getenv("DATABASE_URL"),
		RedisURL:    getenv("REDIS_URL"),
		EngineAddr:  getenv("ENGINE_ADDR"),
		PostHogKey:  getenv("POSTHOG_API_KEY"),
		PostHogHost: defaultValue(getenv("POSTHOG_HOST"), "https://us.i.posthog.com"),
	}
	if value := getenv("ANALYTICS_OPT_OUT"); value != "" {
		var err error
		cfg.AnalyticsOptOut, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("ANALYTICS_OPT_OUT is invalid")
		}
	}
	if value := getenv("LESSON_TEST_TRACER_ENABLED"); value != "" {
		var err error
		cfg.LessonTestTracerEnabled, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("LESSON_TEST_TRACER_ENABLED is invalid")
		}
	}
	for _, v := range []struct{ name, value string }{
		{"DATABASE_URL", cfg.DatabaseURL},
		{"REDIS_URL", cfg.RedisURL},
		{"ENGINE_ADDR", cfg.EngineAddr},
	} {
		if strings.TrimSpace(v.value) == "" {
			return Config{}, fmt.Errorf("%s is required", v.name)
		}
	}
	if _, _, err := net.SplitHostPort(cfg.Addr); err != nil {
		return Config{}, fmt.Errorf("API_ADDR must be host:port")
	}
	if !strings.HasPrefix(cfg.DatabaseURL, "postgres://") && !strings.HasPrefix(cfg.DatabaseURL, "postgresql://") {
		return Config{}, fmt.Errorf("DATABASE_URL must be a postgres URL")
	}
	if _, err := pgxpool.ParseConfig(cfg.DatabaseURL); err != nil {
		return Config{}, fmt.Errorf("DATABASE_URL is invalid")
	}
	if _, err := redis.ParseURL(cfg.RedisURL); err != nil {
		return Config{}, fmt.Errorf("REDIS_URL is invalid")
	}
	if err := validateHTTPURL(cfg.EngineAddr); err != nil {
		return Config{}, fmt.Errorf("ENGINE_ADDR must be an http(s) URL with host:port")
	}
	engineURL, _ := url.Parse(cfg.EngineAddr)
	if _, _, err := net.SplitHostPort(engineURL.Host); err != nil {
		return Config{}, fmt.Errorf("ENGINE_ADDR must include a port")
	}
	if err := validateHTTPURL(cfg.PostHogHost); err != nil {
		return Config{}, fmt.Errorf("POSTHOG_HOST must be an http(s) URL")
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(defaultValue(getenv("LOG_LEVEL"), "info"))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL is invalid")
	}
	return cfg, nil
}
func defaultValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("invalid URL")
	}
	return nil
}
