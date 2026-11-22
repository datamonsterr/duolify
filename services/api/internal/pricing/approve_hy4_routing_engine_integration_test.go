//go:build integration

package pricing

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// The ACTIVE approved version, not just the newest hy4 migration file, matches engine/config/models.toml,
// and the startup check finds no gap when the engine reports provider openrouter for both routes.
func TestActiveApprovedPricingMatchesEngineRoutes(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("active_routes"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	row, err := db.Queries.ActiveQuoteConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(row.Config)
	if err != nil {
		t.Fatal(err)
	}
	var policy QuotePolicy
	if err = json.Unmarshal(row.Policy, &policy); err != nil {
		t.Fatal(err)
	}
	routes := engineRoutes(t, "../../../../engine/config/models.toml")
	checked := 0
	for action, task := range quotedActionTasks {
		if _, ok := cfg.Actions[action]; !ok {
			continue
		}
		route, ok := routes[task]
		if !ok {
			t.Errorf("engine/config/models.toml has no [tasks.%s] for quoted action %s", task, action)
			continue
		}
		key, ok := policy.ActionModels[action]
		if !ok {
			t.Errorf("active %s quotes no model for %s; engine serves %s -> %s", cfg.Version, action, route.model, route.fallback)
			continue
		}
		primary, fallback := policy.Models[key], policy.Models[policy.FallbackModels[key]]
		if primary.Provider != "openrouter" || primary.Model != route.model || fallback.Provider != "openrouter" || fallback.Model != route.fallback {
			t.Errorf("active %s: %s quotes %s/%s -> %s/%s, engine task %s serves openrouter/%s -> %s", cfg.Version, action, primary.Provider, primary.Model, fallback.Provider, fallback.Model, task, route.model, route.fallback)
		}
		checked++
	}
	if checked < 8 {
		t.Fatalf("only %d quoted actions compared with the engine for %s", checked, cfg.Version)
	}

	var logs bytes.Buffer
	CheckActiveRoutes(ctx, db.Pool, slog.New(slog.NewJSONHandler(&logs, nil)), func(context.Context) ([]string, error) {
		return []string{"openrouter", "openrouter"}, nil
	})
	if logs.Len() != 0 {
		t.Fatalf("startup route check reported: %s", logs.String())
	}
}
