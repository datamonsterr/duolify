//go:build integration

package gamification

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestRolloverIsDrivenByIndexedNextRolloverAt(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	svc := New(db.Pool, nil)
	clock := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return clock }

	learner := func(zone string) string {
		t.Helper()
		var id string
		if err := db.Pool.QueryRow(ctx, `INSERT INTO learners(display_name) VALUES('rollover') RETURNING id::text`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO learner_profiles(learner_id,time_zone) VALUES($1,$2)`, id, zone); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO streak_state(learner_id,decided_through) VALUES($1,'2026-11-10')`, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	nextAt := func(id string) time.Time {
		t.Helper()
		var at time.Time
		if err := db.Pool.QueryRow(ctx, `SELECT next_rollover_at FROM streak_state WHERE learner_id=$1`, id).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at.UTC()
	}
	decided := func(id string) string {
		t.Helper()
		var d string
		if err := db.Pool.QueryRow(ctx, `SELECT decided_through::text FROM streak_state WHERE learner_id=$1`, id).Scan(&d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	kiritimati := learner("Pacific/Kiritimati") // UTC+14
	pago := learner("Pacific/Pago_Pago")        // UTC-11
	utc := learner("UTC")

	// Due when the learner's own clock reads decided_through + 2 (the day decided_through + 1 has ended).
	for id, want := range map[string]time.Time{
		kiritimati: time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC),
		utc:        time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC),
		pago:       time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC),
	} {
		if got := nextAt(id); !got.Equal(want) {
			t.Errorf("next_rollover_at=%s, want %s", got, want)
		}
	}

	clock = time.Date(2026, 11, 11, 9, 59, 0, 0, time.UTC)
	if n, err := svc.rolloverDue(ctx); err != nil || n != 0 {
		t.Fatalf("before any local midnight: judged %d, err %v", n, err)
	}
	clock = time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	if n, err := svc.rolloverDue(ctx); err != nil || n != 1 || decided(kiritimati) == "2026-11-10" {
		t.Fatalf("Kiritimati midnight: judged %d, err %v, decided_through %s", n, err, decided(kiritimati))
	}
	if !nextAt(kiritimati).After(clock) {
		t.Errorf("a rolled-over learner stays due: next_rollover_at=%s", nextAt(kiritimati))
	}
	if n, err := svc.rolloverDue(ctx); err != nil || n != 0 {
		t.Fatalf("second pass judged %d, err %v, want 0", n, err)
	}
	if decided(utc) != "2026-11-10" || decided(pago) != "2026-11-10" {
		t.Fatal("learners whose day has not ended were judged")
	}

	// A learner who moves zone is due by the new zone's clock.
	if _, err := db.Pool.Exec(ctx, `UPDATE learner_profiles SET time_zone='Pacific/Kiritimati' WHERE learner_id=$1`, utc); err != nil {
		t.Fatal(err)
	}
	if got, want := nextAt(utc), time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("after the zone change next_rollover_at=%s, want %s", got, want)
	}

	// Two workers at once judge each due learner exactly once.
	clock = time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := svc.Rollover(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for _, id := range []string{utc, pago, kiritimati} {
		if !nextAt(id).After(clock) {
			t.Errorf("learner %s is still due after the pass: next_rollover_at=%s decided=%s", id, nextAt(id), decided(id))
		}
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SET LOCAL enable_seqscan=off"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN SELECT learner_id::text FROM streak_state WHERE next_rollover_at<=$1::timestamptz ORDER BY next_rollover_at,learner_id LIMIT 200`, clock)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	rows.Close()
	if !strings.Contains(plan.String(), "streak_state_next_rollover") {
		t.Fatalf("the due predicate does not use its index:\n%s", plan.String())
	}
}
