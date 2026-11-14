//go:build integration

package sharing_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	sharingAdmin *pgxpool.Pool
	sharingURL   *url.URL
	sharingDBSeq atomic.Uint64
)

// Sharing exercises SQL plus explicit storage, engine and push fakes. One
// migrated Postgres template gives every fixture its own database without
// starting unrelated backup workers, RustFS containers and IAM provisioning.
func TestMain(m *testing.M) {
	os.Exit(runSharingTests(m))
}

func runSharingTests(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("synthetic-only"), postgres.BasicWaitStrategies())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := container.Terminate(cleanup); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err == nil {
		err = migrations.Up(ctx, dsn)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	template, err := store.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	err = worker.Migrate(ctx, template.Pool, slog.Default())
	template.Close() // CREATE DATABASE needs a template with no open connections.
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sharingURL, err = url.Parse(dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sharingURL.Path = "/postgres"
	sharingAdmin, err = pgxpool.New(ctx, sharingURL.String())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer sharingAdmin.Close()
	return m.Run()
}

func newSharingFixture(t *testing.T) *backuptest.Fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	name := fmt.Sprintf("sharing_%d", sharingDBSeq.Add(1))
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := sharingAdmin.Exec(ctx, "CREATE DATABASE "+identifier+" TEMPLATE duolify"); err != nil {
		t.Fatal(err)
	}
	var db *store.Store
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := sharingAdmin.Exec(cleanup, "DROP DATABASE "+identifier); err != nil {
			t.Error(err)
		}
	})
	dsn := *sharingURL
	dsn.Path = "/" + name
	params := dsn.Query()
	params.Set("pool_max_conns", "4") // Exercise a small CI-sized pool on any host.
	dsn.RawQuery = params.Encode()
	var err error
	db, err = store.New(ctx, dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	return &backuptest.Fixture{DB: db}
}

func newSeededSharingFixture(t *testing.T) *backuptest.Fixture {
	t.Helper()
	f := newSharingFixture(t)
	f.SeedDatabase(t, &memObjects{puts: map[string][]byte{}})
	return f
}
