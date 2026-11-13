package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, tt := range []struct{ name, key, value, want string }{
		{name: "defaults"},
		{"missing postgres", "DATABASE_URL", "", "DATABASE_URL"},
		{"missing redis", "REDIS_URL", "", "REDIS_URL"},
		{"missing engine", "ENGINE_ADDR", "", "ENGINE_ADDR"},
		{"bad postgres", "DATABASE_URL", "garbage", "DATABASE_URL"},
		{"bad redis", "REDIS_URL", "http://localhost", "REDIS_URL"},
		{"bad engine", "ENGINE_ADDR", "ftp://localhost", "ENGINE_ADDR"},
		{"bad listen", "API_ADDR", "invalid", "API_ADDR"},
		{"bad log level", "LOG_LEVEL", "bogus", "LOG_LEVEL"},
		{"bad analytics opt out", "ANALYTICS_OPT_OUT", "bogus", "ANALYTICS_OPT_OUT"},
		{"analytics opt out", "ANALYTICS_OPT_OUT", "true", ""},
		{"bad tracer flag", "LESSON_TEST_TRACER_ENABLED", "bogus", "LESSON_TEST_TRACER_ENABLED"},
		{"tracer enabled", "LESSON_TEST_TRACER_ENABLED", "true", ""},
		{"tls engine", "ENGINE_ADDR", "https://engine:50051", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{"DATABASE_URL": "postgres://localhost:5432/local", "REDIS_URL": "redis://localhost:6379/0", "ENGINE_ADDR": "http://localhost:50051"}
			if tt.key != "" {
				env[tt.key] = tt.value
			}
			cfg, err := Load(func(key string) string { return env[key] })
			if tt.want != "" {
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("error=%v, want %s", err, tt.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Addr != ":8080" || cfg.PostHogHost != "https://us.i.posthog.com" {
				t.Fatalf("bad defaults: %+v", cfg)
			}
			if tt.key == "ANALYTICS_OPT_OUT" && tt.value == "true" && !cfg.AnalyticsOptOut {
				t.Fatal("analytics opt-out ignored")
			}
			if tt.key == "LESSON_TEST_TRACER_ENABLED" && tt.value == "true" && !cfg.LessonTestTracerEnabled {
				t.Fatal("lesson test tracer enabled ignored")
			}
			if tt.name == "defaults" && cfg.LessonTestTracerEnabled {
				t.Fatal("lesson test tracer enabled should default to false")
			}
		})
	}
}
