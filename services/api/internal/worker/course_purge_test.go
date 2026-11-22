package worker

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

type purgerDouble struct {
	cutoffs []time.Time
	n       int64
}

func (p *purgerDouble) PurgeDeletedCourses(_ context.Context, cutoff time.Time) (int64, error) {
	p.cutoffs = append(p.cutoffs, cutoff)
	return p.n, nil
}

func TestCoursePurgeRegistersOneStableDailyJob(t *testing.T) {
	p := new(purgerDouble)
	jobs := registerCoursePurgeWorker(river.NewWorkers(), p)
	if len(jobs) != 1 {
		t.Fatalf("want 1 periodic job, got %d", len(jobs))
	}
	if got := registerCoursePurgeWorker(river.NewWorkers(), struct{}{}); got != nil {
		t.Fatal("service without a purger must not register a job")
	}
}

func TestCoursePurgeArgsAreUniquePerDay(t *testing.T) {
	if (CoursePurgeArgs{}).Kind() != "course_purge" {
		t.Fatal("unexpected kind")
	}
	opts := (CoursePurgeArgs{}).InsertOpts()
	if opts.UniqueOpts.ByPeriod != 24*time.Hour {
		t.Fatalf("job must be unique per day so several workers insert it once: %+v", opts)
	}
}

func TestCoursePurgeWorkerUsesThirtyDayCutoff(t *testing.T) {
	p := &purgerDouble{n: 2}
	before := time.Now().UTC()
	if err := (&CoursePurgeWorker{Service: p}).Work(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if len(p.cutoffs) != 1 {
		t.Fatal("purge not called once")
	}
	want := before.Add(-CourseDeletionGrace)
	if d := p.cutoffs[0].Sub(want); d < 0 || d > time.Minute {
		t.Fatalf("cutoff %v not ~30 days ago (%v)", p.cutoffs[0], want)
	}
	if CourseDeletionGrace != 30*24*time.Hour {
		t.Fatal("grace must be 30 days")
	}
}
