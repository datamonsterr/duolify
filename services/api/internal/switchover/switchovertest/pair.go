// Package switchovertest starts two Postgres 18 instances joined by logical replication.
package switchovertest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/network"
)

// Subscription is the subscription name on the target database.
const Subscription = "duolify_switchover"

// Pair is a publisher (Source) and a subscriber (Target) on one private Docker network.
type Pair struct{ SourceURL, TargetURL string }

func start(ctx context.Context, t testing.TB, nw *testcontainers.DockerNetwork, alias string) string {
	t.Helper()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"),
		testcontainers.WithCmd("postgres", "-c", "wal_level=logical"), network.WithNetwork([]string{alias}, nw), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, c)
	url, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return url
}

// New creates both databases with schema, publishes every table from Source and subscribes Target
// without an initial copy, so both start empty and identical. Each prepare func runs on both databases after schema. It returns when replication is streaming.
func New(ctx context.Context, t testing.TB, schema string, prepare ...func(url string)) *Pair {
	t.Helper()
	nw, err := network.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupNetwork(t, nw)
	p := &Pair{SourceURL: start(ctx, t, nw, "source"), TargetURL: start(ctx, t, nw, "target")}
	for _, url := range []string{p.SourceURL, p.TargetURL} {
		Exec(ctx, t, url, schema)
		for _, fn := range prepare {
			fn(url)
		}
	}
	Exec(ctx, t, p.SourceURL, "CREATE PUBLICATION "+Subscription+" FOR ALL TABLES")
	Exec(ctx, t, p.TargetURL, fmt.Sprintf("CREATE SUBSCRIPTION %s CONNECTION 'host=source dbname=duolify user=duolify password=test-only' PUBLICATION %[1]s WITH (copy_data=false)", Subscription))
	deadline := time.Now().Add(30 * time.Second)
	for {
		var n int
		if err := QueryRow(ctx, t, p.TargetURL, "SELECT count(*) FROM pg_stat_subscription WHERE subname=$1 AND relid IS NULL AND pid IS NOT NULL", Subscription).Scan(&n); err == nil && n == 1 {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatal("subscription did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Exec runs one statement with a short-lived connection.
func Exec(ctx context.Context, t testing.TB, url, sql string, args ...any) {
	t.Helper()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err = conn.Exec(ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}

type row struct {
	conn *pgx.Conn
	pgx.Row
	ctx context.Context
}

func (r row) Scan(dest ...any) error {
	defer func() { _ = r.conn.Close(r.ctx) }()
	return r.Row.Scan(dest...)
}

// QueryRow runs a one-row query with a short-lived connection that closes on Scan.
func QueryRow(ctx context.Context, t testing.TB, url, sql string, args ...any) pgx.Row {
	t.Helper()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	return row{conn: conn, Row: conn.QueryRow(ctx, sql, args...), ctx: ctx}
}
