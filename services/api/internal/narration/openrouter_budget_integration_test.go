//go:build integration

package narration

import (
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestOpenRouterBudgetConcurrentUTCBillingCycleAdmissionsAndUnknownOutcomes(t *testing.T) {
	ctx := t.Context()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("openrouter"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	t.Cleanup(db.Close)
	config := Options{Pool: db.Pool, OpenRouterBudget: OpenRouterBudget{MonthlyCapNanos: 9000}}
	services := []*Service{New(config), New(config)}
	edge := time.Date(2026, time.November, 30, 23, 59, 59, 0, time.UTC)
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	for _, service := range services {
		wg.Go(func() {
			id, _, err := service.reserveOpenRouter(ctx, 5000, edge)
			if err != nil {
				t.Error(err)
			}
			ids <- id
		})
	}
	wg.Wait()
	close(ids)
	var first string
	for id := range ids {
		if id != "" {
			if first != "" {
				t.Fatal("both processes exceeded the same account allowance")
			}
			first = id
		}
	}
	if first == "" {
		t.Fatal("neither request admitted")
	}
	if id, used, e := services[1].reserveOpenRouter(ctx, 4001, edge); e != nil || id != "" || used != 5000 {
		t.Fatalf("unknown submission: ID %q, used %d, err %v", id, used, e)
	}
	next := edge.Add(time.Second)
	id, used, err := services[1].reserveOpenRouter(ctx, 9000, next)
	if err != nil || id == "" || used != 0 {
		t.Fatalf("next UTC month: ID %q, used %d, err %v", id, used, err)
	}
	for _, actual := range []int64{250, 0} {
		if err = services[0].settleOpenRouter(ctx, id, actual); err != nil {
			t.Fatal(err)
		}
	}
	if id, used, e := services[1].reserveOpenRouter(ctx, 8750, next); e != nil || id == "" || used != 250 {
		t.Fatalf("reconciled month: ID %q, used %d, err %v", id, used, e)
	}
}
