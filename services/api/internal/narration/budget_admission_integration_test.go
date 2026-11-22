//go:build integration

package narration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestConcurrentBudgetAdmissionsAcrossServices(t *testing.T) {
	ctx := context.Background()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("budget_race"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, c)
	url, err := c.ConnectionString(ctx, "sslmode=disable")
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
	b := Budget{Limit: 40000, StopPercent: 95, CycleDay: 1}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO narration_budget_ledger(characters) VALUES(37000)`); err != nil {
		t.Fatal(err)
	}
	services := []*Service{New(Options{Pool: db.Pool, Budget: b}), New(Options{Pool: db.Pool, Budget: b})}
	start := make(chan struct{})
	var wg sync.WaitGroup
	admitted := make(chan bool, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			<-start
			id, _, e := s.reserveBudget(ctx, 1000, time.Now())
			if e != nil {
				errs <- e
				return
			}
			admitted <- id != ""
		}(services[i])
	}
	close(start)
	wg.Wait()
	close(admitted)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	n := 0
	for ok := range admitted {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("admitted %d, want 1", n)
	}
}

// Many callers across two service instances never reserve past the stop line, and a reservation
// whose process died (never settled) keeps counting.
func TestManyConcurrentReservationsNeverExceedStopLine(t *testing.T) {
	ctx := context.Background()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("budget_many"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, c)
	url, err := c.ConnectionString(ctx, "sslmode=disable")
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
	b := Budget{Limit: 40000, StopPercent: 95, CycleDay: 1}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO narration_budget_ledger(characters) VALUES(30000)`); err != nil {
		t.Fatal(err)
	}
	services := []*Service{New(Options{Pool: db.Pool, Budget: b}), New(Options{Pool: db.Pool, Budget: b})}
	const callers = 24
	start := make(chan struct{})
	var wg sync.WaitGroup
	var admitted atomic.Int64
	var failed atomic.Int64
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			<-start
			id, _, e := s.reserveBudget(ctx, 1000, time.Now())
			if e != nil {
				failed.Add(1)
			} else if id != "" {
				admitted.Add(1)
			}
		}(services[i%2])
	}
	close(start)
	wg.Wait()
	if failed.Load() != 0 || admitted.Load() != 8 {
		t.Fatalf("admitted %d failed %d, want 8 and 0", admitted.Load(), failed.Load())
	}
	used, err := services[0].CharactersUsed(ctx, b.CycleStart(time.Now()))
	if err != nil || used != 38000 {
		t.Fatalf("used %d %v, want 38000 (unsettled reservations stay counted)", used, err)
	}
	// Settling releases the unused part once; a second settle or a larger one is ignored.
	id, _, err := services[0].reserveBudget(ctx, 0, time.Now())
	if err != nil || id == "" {
		t.Fatalf("zero reserve: %q %v", id, err)
	}
	var one string
	if err = db.Pool.QueryRow(ctx, `SELECT id::text FROM narration_budget_ledger WHERE characters=1000 LIMIT 1`).Scan(&one); err != nil {
		t.Fatal(err)
	}
	if err = services[0].settleBudget(ctx, one, 400); err != nil {
		t.Fatal(err)
	}
	if err = services[1].settleBudget(ctx, one, 0); err != nil {
		t.Fatal(err)
	}
	if err = services[1].settleBudget(ctx, one, 5000); err != nil {
		t.Fatal(err)
	}
	if used, err = services[0].CharactersUsed(ctx, b.CycleStart(time.Now())); err != nil || used != 37400 {
		t.Fatalf("after settle used %d %v, want 37400", used, err)
	}
	if _, err = db.Pool.Exec(ctx, `DELETE FROM narration_budget_ledger`); err == nil {
		t.Fatal("ledger rows must be immutable")
	}
}
