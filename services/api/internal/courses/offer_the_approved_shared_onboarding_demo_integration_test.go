//go:build integration

package courses_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestSharedDemoIntegration(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("demo"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	database, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	snapshot := func() map[string]string {
		t.Helper()
		rows, err := database.Pool.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		out := map[string]string{}
		for _, name := range names {
			var raw string
			if err := database.Pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM "`+name+`" r`).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			out[name] = raw
		}
		return out
	}
	before := snapshot()
	// No engine, financial, learner or pricing dependencies: no provider call is possible.
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	for range 3 {
		request, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/v1/courses/shared-demo", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var demo courses.SharedDemo
		err = json.NewDecoder(response.Body).Decode(&demo)
		_ = response.Body.Close()
		if response.StatusCode != 200 || err != nil || demo.Version == "" || len(demo.Preview.ExampleSlides) == 0 {
			t.Fatalf("demo unavailable: %d %v", response.StatusCode, err)
		}
	}
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("demo wrote persisted state")
	}
}
