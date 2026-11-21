//go:build integration

package switchover_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/switchover"
	"github.com/datamonsterr/duolify/services/api/internal/switchover/switchovertest"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/riverqueue/river"
)

const schema = `CREATE TABLE items(id bigserial PRIMARY KEY, v text NOT NULL); CREATE TABLE tags(id serial PRIMARY KEY, n int NOT NULL);`

func logger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func newPair(t *testing.T) (*switchovertest.Pair, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	t.Cleanup(cancel)
	migrateRiver := func(url string) {
		s, err := store.New(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if err = worker.Migrate(ctx, s.Pool, logger()); err != nil {
			t.Fatal(err)
		}
	}
	return switchovertest.New(ctx, t, schema, migrateRiver), ctx
}

// stack builds one API stack: gate, pool on url and a River client under the gate.
func stack(ctx context.Context, t *testing.T, url string, opts switchover.Options) (*switchover.Gate, *store.Store, *river.Client[pgx.Tx]) {
	t.Helper()
	opts.SourceURL = url
	if opts.Subscription == "" {
		opts.Subscription = switchovertest.Subscription
	}
	g, err := switchover.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url, g.PoolConfig)
	if err != nil {
		t.Fatal(err)
	}
	g.Attach(db.Pool)
	t.Cleanup(db.Close)
	client, err := worker.New(db.Pool, logger())
	if err != nil {
		t.Fatal(err)
	}
	if err = g.Start(ctx, client); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Stop(context.WithoutCancel(ctx)) })
	return g, db, client
}

// serverPort names the cluster the pool is talking to (both containers listen on 5432 internally).
func serverPort(ctx context.Context, t *testing.T, db *store.Store) int64 {
	t.Helper()
	var id int64
	if err := db.Pool.QueryRow(ctx, "SELECT system_identifier FROM pg_control_system()").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func urlPort(ctx context.Context, t *testing.T, url string) int64 {
	t.Helper()
	var id int64
	if err := switchovertest.QueryRow(ctx, t, url, "SELECT system_identifier FROM pg_control_system()").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func jobDone(ctx context.Context, t *testing.T, db *store.Store, id int64) bool {
	t.Helper()
	var state string
	if err := db.Pool.QueryRow(ctx, "SELECT state::text FROM river_job WHERE id=$1", id).Scan(&state); err != nil {
		return false
	}
	return state == "completed"
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestSwitchWaitsForSessionLockHolderThenSwapsPoolAndRestartsRiver(t *testing.T) {
	pair, ctx := newPair(t)
	const margin = 500
	g, db, client := stack(ctx, t, pair.SourceURL, switchover.Options{TargetURL: pair.TargetURL, SequenceMargin: margin, HoldMax: 15 * time.Second})
	var lastID int64
	for range 5 {
		if err := db.Pool.QueryRow(ctx, "INSERT INTO items(v) VALUES('before') RETURNING id").Scan(&lastID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO tags(n) SELECT g FROM generate_series(1,3) g"); err != nil {
		t.Fatal(err)
	}
	srcPort := serverPort(ctx, t, db)

	// A narration-style request: a pooled connection holding a session advisory lock.
	lockCtx := g.Admit(ctx)
	lock, err := db.Pool.Acquire(lockCtx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lock.Exec(lockCtx, "SELECT pg_advisory_lock(4242)"); err != nil {
		t.Fatal(err)
	}
	switched := make(chan error, 1)
	go func() { switched <- g.Switch(ctx) }()
	eventually(t, "gate to close", func() bool { return g.State() == switchover.StateSwitching })
	// New work is held, not rejected, while the lock holder is still in flight.
	insertDone := make(chan int64, 1)
	go func() {
		var id int64
		_ = db.Pool.QueryRow(g.Admit(ctx), "INSERT INTO items(v) VALUES('held') RETURNING id").Scan(&id)
		insertDone <- id
	}()
	select {
	case err := <-switched:
		t.Fatalf("switch finished while a session advisory lock was held: %v", err)
	case <-insertDone:
		t.Fatal("new work ran while the gate was closed")
	case <-time.After(300 * time.Millisecond):
	}
	// The holder can still use the pool for nested queries, then releases.
	if _, err = db.Pool.Exec(lockCtx, "SELECT 1"); err != nil {
		t.Fatalf("nested query of an in-flight request was held: %v", err)
	}
	if _, err = lock.Exec(lockCtx, "SELECT pg_advisory_unlock(4242)"); err != nil {
		t.Fatal(err)
	}
	lock.Release()
	if err = <-switched; err != nil {
		t.Fatal(err)
	}
	if id := <-insertDone; id <= lastID+margin {
		t.Fatalf("held insert id %d must be above publisher last value %d + margin %d", id, lastID, margin)
	}
	if got, want := serverPort(ctx, t, db), urlPort(ctx, t, pair.TargetURL); got != want || got == srcPort {
		t.Fatalf("pool serves cluster %d, want target %d (source %d)", got, want, srcPort)
	}
	var tagID int
	if err = db.Pool.QueryRow(ctx, "INSERT INTO tags(n) VALUES(1) RETURNING id").Scan(&tagID); err != nil || tagID <= 3+margin {
		t.Fatalf("serial sequence not synced with margin: id=%d err=%v", tagID, err)
	}
	var n int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE v='before'").Scan(&n); err != nil || n != 5 {
		t.Fatalf("replicated rows = %d err=%v", n, err)
	}
	// Old database refuses writes so nothing lands there after the swap.
	var pgErr *pgconn.PgError
	_, err = pgxExec(ctx, t, pair.SourceURL, "INSERT INTO items(v) VALUES('late')")
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("old database write error = %v, want read-only 25006", err)
	}
	// River was stopped and restarted on the new database.
	res, err := client.Insert(ctx, worker.DummyArgs{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "River job to run on the new database", func() bool { return jobDone(ctx, t, db, res.Job.ID) })
}

func pgxExec(ctx context.Context, t *testing.T, url, sql string) (pgconn.CommandTag, error) {
	t.Helper()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	return conn.Exec(ctx, sql)
}

func TestForcedLagTimeoutReleasesBackToOldPool(t *testing.T) {
	pair, ctx := newPair(t)
	g, db, client := stack(ctx, t, pair.SourceURL, switchover.Options{TargetURL: pair.TargetURL, HoldMax: 15 * time.Second, LagTimeout: time.Second})
	srcPort := serverPort(ctx, t, db)
	// Disable the subscription and write on: the subscriber can never catch up.
	switchovertest.Exec(ctx, t, pair.TargetURL, "ALTER SUBSCRIPTION "+switchovertest.Subscription+" DISABLE")
	if _, err := db.Pool.Exec(ctx, "INSERT INTO items(v) VALUES('unreplicated')"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err := g.Switch(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want lag timeout", err)
	}
	if time.Since(started) > 15*time.Second {
		t.Fatalf("pause %v exceeds 15s", time.Since(started))
	}
	if g.State() != switchover.StateOpen || g.Switched() {
		t.Fatalf("state=%s switched=%v", g.State(), g.Switched())
	}
	var ro string
	if err = switchovertestRow(ctx, t, pair.SourceURL, "SHOW default_transaction_read_only", &ro); err != nil || ro != "off" {
		t.Fatalf("read-only not cleared: %q %v", ro, err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO items(v) VALUES('after rollback')"); err != nil {
		t.Fatalf("old pool must accept writes after rollback: %v", err)
	}
	if got := serverPort(ctx, t, db); got != srcPort {
		t.Fatalf("pool moved to cluster %d", got)
	}
	res, err := client.Insert(ctx, worker.DummyArgs{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "River to resume on the old pool", func() bool { return jobDone(ctx, t, db, res.Job.ID) })
}

func switchovertestRow(ctx context.Context, t *testing.T, url, sql string, dest any) error {
	t.Helper()
	return switchovertest.QueryRow(ctx, t, url, sql).Scan(dest)
}

func TestStandbyStackOpensOnTriggerAndOldStackDrainsThroughNewPool(t *testing.T) {
	pair, ctx := newPair(t)
	oldGate, oldDB, _ := stack(ctx, t, pair.SourceURL, switchover.Options{TargetURL: pair.TargetURL, HoldMax: 15 * time.Second})
	newGate, newDB, newClient := stack(ctx, t, pair.TargetURL, switchover.Options{Standby: true})
	if newGate.State() != switchover.StateStandby {
		t.Fatalf("state = %s", newGate.State())
	}
	// River is off in standby: a job inserted through the new stack stays queued.
	res, err := newClient.Insert(switchover.Bypass(ctx), worker.DummyArgs{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if jobDone(switchover.Bypass(ctx), t, newDB, res.Job.ID) {
		t.Fatal("River ran in standby")
	}
	held := make(chan error, 1)
	go func() {
		_, err := newDB.Pool.Exec(newGate.Admit(ctx), "INSERT INTO tags(n) VALUES(2)")
		held <- err
	}()
	select {
	case err := <-held:
		t.Fatalf("standby served a request: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	// Compute and database move together: switch the old stack, then open the new one.
	oldHeld := make(chan error, 1)
	if err = oldGate.Switch(ctx); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, err := oldDB.Pool.Exec(oldGate.Admit(ctx), "INSERT INTO tags(n) VALUES(3)")
		oldHeld <- err
	}()
	if err = newGate.Open(ctx); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []chan error{held, oldHeld} {
		if err = <-ch; err != nil {
			t.Fatalf("held request failed: %v", err)
		}
	}
	if got, want := serverPort(ctx, t, oldDB), serverPort(ctx, t, newDB); got != want {
		t.Fatalf("old stack serves cluster %d, new stack %d", got, want)
	}
	eventually(t, "queued River job to run once the standby opens", func() bool { return jobDone(ctx, t, newDB, res.Job.ID) })
}

func TestReadOnlyModeServesReadsRejectsWritesAndStopsRiver(t *testing.T) {
	pair, ctx := newPair(t)
	g, db, client := stack(ctx, t, pair.SourceURL, switchover.Options{})
	if _, err := db.Pool.Exec(ctx, "INSERT INTO items(v) VALUES('seed')"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /read", func(w http.ResponseWriter, r *http.Request) {
		var n int
		if err := db.Pool.QueryRow(r.Context(), "SELECT count(*) FROM items").Scan(&n); err != nil {
			http.Error(w, err.Error(), 500)
		}
	})
	mux.HandleFunc("POST /write", func(w http.ResponseWriter, r *http.Request) {
		if _, err := db.Pool.Exec(r.Context(), "INSERT INTO items(v) VALUES('w')"); err != nil {
			http.Error(w, err.Error(), 500)
		}
	})
	srv := httptest.NewServer(g.Middleware(mux))
	t.Cleanup(srv.Close)
	do := func(method, path string) (code int, retryAfter string) {
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+path, nil)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode, resp.Header.Get("Retry-After")
	}
	if err := g.EnterReadOnly(ctx); err != nil {
		t.Fatal(err)
	}
	if code, _ := do("GET", "/read"); code != 200 {
		t.Fatalf("read = %d", code)
	}
	if code, retryAfter := do("POST", "/write"); code != 503 || retryAfter == "" {
		t.Fatalf("write = %d retry-after=%q", code, retryAfter)
	}
	// Even a write that bypasses HTTP is refused by the database.
	var pgErr *pgconn.PgError
	if _, err := db.Pool.Exec(ctx, "INSERT INTO items(v) VALUES('direct')"); !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("direct write error = %v, want 25006", err)
	}
	// River is stopped: a job stays available.
	select {
	case <-client.Stopped():
	default:
		t.Fatal("River still running in read-only mode")
	}
	if err := g.LeaveReadOnly(ctx); err != nil {
		t.Fatal(err)
	}
	if code, _ := do("POST", "/write"); code != 200 {
		t.Fatalf("write after leaving = %d", code)
	}
	res, err := client.Insert(ctx, worker.DummyArgs{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "River to resume", func() bool { return jobDone(ctx, t, db, res.Job.ID) })
}
