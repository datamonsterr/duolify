package worker

import (
	"context"
	"testing"

	"github.com/riverqueue/river"
)

type fakeEraser struct {
	enabled bool
	calls   int
}

func (f *fakeEraser) Enabled() bool { return f.enabled }
func (f *fakeEraser) ProcessClerkUserDeletions(context.Context) (int, error) {
	f.calls++
	return 1, nil
}

func TestClerkUserDeletionIsScheduledOnlyWhenClerkIsConfigured(t *testing.T) {
	if jobs := registerClerkUserDeletionWorker(river.NewWorkers(), &fakeEraser{enabled: false}, nil); len(jobs) != 0 {
		t.Fatal("scheduled without Clerk")
	}
	if jobs := registerClerkUserDeletionWorker(river.NewWorkers(), struct{}{}, nil); len(jobs) != 0 {
		t.Fatal("scheduled for a service without the capability")
	}
	if jobs := registerClerkUserDeletionWorker(river.NewWorkers(), &fakeEraser{enabled: true}, nil); len(jobs) != 1 {
		t.Fatal("not scheduled with Clerk")
	}
}

func TestClerkUserDeletionWorkerRunsTheEraser(t *testing.T) {
	eraser := &fakeEraser{enabled: true}
	w := &ClerkUserDeletionWorker{Service: eraser}
	if err := w.Work(t.Context(), nil); err != nil || eraser.calls != 1 {
		t.Fatal(err, eraser.calls)
	}
	if opts := (ClerkUserDeletionArgs{}).InsertOpts(); opts.MaxAttempts != 1 {
		t.Fatal("a failed run must wait for the next scheduled one")
	}
}
