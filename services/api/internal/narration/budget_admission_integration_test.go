//go:build integration

package narration

import (
	"context"
	"sync"
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
