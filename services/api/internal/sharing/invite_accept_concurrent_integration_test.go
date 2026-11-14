//go:build integration

package sharing_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

// Regression for #212. An in-flight enrollment holds FOR KEY SHARE on the recipient's and the
// sender's learner rows (foreign keys of the copy rows). Accept must not take a stronger learner
// lock than FOR NO KEY UPDATE: FOR UPDATE queued behind that enrollment while holding the other
// learner row, which deadlocked against the enrollment and aborted one accept with no copy id.
func TestInviteAcceptDoesNotBlockOnInFlightEnrollment(t *testing.T) {
	e := inviteWorld(t, nil)
	id := e.person(t, "Inflight")
	e.befriend(t, owner, id)
	inv, err := e.s.SendInvite(t.Context(), owner, e.src, sharing.InviteInput{Username: "inflight"})
	if err != nil {
		t.Fatal(err)
	}
	enrolling, err := e.f.DB.Pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = enrolling.Rollback(context.WithoutCancel(t.Context())) }()
	if _, err = enrolling.Exec(t.Context(), `SELECT id FROM learners WHERE id IN ($1::uuid,$2::uuid) FOR KEY SHARE`, id, owner); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	out, err := e.s.AcceptInvite(ctx, id, inv.ID)
	if err != nil || out.CourseID == "" {
		t.Fatalf("accept must not wait for an in-flight enrollment: %+v %v", out, err)
	}
}

// Many accepts of one invite all succeed with the one copy.
func TestConcurrentInviteAcceptsAllReturnTheOneCopy(t *testing.T) {
	e := inviteWorld(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	const racers = 12
	id := e.person(t, "Race")
	e.befriend(t, owner, id)
	inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "race"})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	outs := make([]sharing.InviteOutcome, racers)
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			outs[i], errs[i] = e.s.AcceptInvite(ctx, id, inv.ID)
		}()
	}
	close(start)
	wg.Wait()
	for i := range racers {
		if errs[i] != nil || outs[i].CourseID == "" || outs[i].CourseID != outs[0].CourseID {
			t.Fatalf("accept %d: out=%+v err=%v (first=%+v)", i, outs[i], errs[i], outs[0])
		}
	}
	if n := e.n(t, `SELECT count(*) FROM course_copies WHERE learner_id=$1`, id); n != 1 {
		t.Fatalf("copies = %d", n)
	}
}
