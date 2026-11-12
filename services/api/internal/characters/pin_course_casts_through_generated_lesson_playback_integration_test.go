//go:build integration

package characters_test

import (
	"encoding/json"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"testing"
)

func TestGeneratedCastTransactionRollbackAndRetry(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	ctx := t.Context()
	owner, course := "a1111111-1111-4111-8111-111111111111", "f1111111-1111-4111-8111-111111111111"
	s := characters.NewService(f.DB.Pool, f.ApplicationStorage(), f.Events())
	if _, err := s.SelectCast(ctx, owner, course, characters.CastSelection{Guide: "default-guide", Supporting: []string{"default-companion"}}); err != nil {
		t.Fatal(err)
	}
	tx, err := f.DB.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.PinJobCastTx(ctx, tx, owner, course, "job-atomic")
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = f.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM lesson_cast_snapshots WHERE lesson_key='job-atomic'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rollback leaked snapshot: %d %v", n, err)
	}
	tx, err = f.DB.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := s.PinJobCastTx(ctx, tx, owner, course, "job-atomic")
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SelectCast(ctx, owner, course, characters.CastSelection{}); err != nil {
		t.Fatal(err)
	}
	tx, err = f.DB.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.PinJobCastTx(ctx, tx, owner, course, "job-atomic")
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(pinned)
	b, _ := json.Marshal(again)
	c, _ := json.Marshal(first)
	if string(a) != string(b) || string(a) != string(c) {
		t.Fatal("retry changed immutable cast")
	}
}
