//go:build integration

package sharing_test

import (
	"context"
	"encoding/json"
	"errors"
	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func TestRateAndScreenCourseReviews(t *testing.T) {
	f := backuptest.New(t)
	ev := &capture{}
	s := marketService(t, f, ev, nil, nil)
	s.ConfigureReviewScreening(startEngine(t).(sharing.ReviewScreener))
	o := marketOwner(t, f, "reviews-owner")
	l := listCourse(t, s, o, marketCourse(t, f, o, "Reviews course", "Description", 3), "tech", "en")
	learner := newLearner(t, f, "reviewer")
	if _, err := s.EnrollMarketplace(t.Context(), learner, l.CourseID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	pass := func(h http.Handler) http.Handler { return h }
	sharing.Mount(mux, s, pass, pass)
	req := httptest.NewRequestWithContext(accounts.WithLearnerID(t.Context(), learner), http.MethodPut, "/v1/marketplace/courses/"+l.CourseID+"/review", strings.NewReader(`{"stars":5,"text":"Helpful"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"lessons_needed":3`) {
		t.Fatalf("early review = %d %s; want 422 with 3 lessons needed", rec.Code, rec.Body)
	}

	consent(t, f, learner, false)
	completeReviewLessons(t, f, learner, l.CourseID)
	saved, err := s.SaveReview(t.Context(), learner, l.CourseID, 5, "Helpful private-review-text")
	if err != nil || saved.Verdict != "pass" || saved.Version != 1 {
		t.Fatalf("save=%+v %v", saved, err)
	}
	assertReviewAggregate(t, f, l.CourseID, 5, 1)
	page, err := s.MarketCourse(t.Context(), learner, l.CourseID)
	if err != nil || page.RatingCount != 1 || page.RatingAverage == nil || *page.RatingAverage != 5 {
		t.Fatalf("marketplace did not consume real review aggregate: %+v %v", page, err)
	}

	// Copy update does not rewrite the version originally reviewed.
	if _, err = f.DB.Pool.Exec(t.Context(), `UPDATE course_copies SET version=2 WHERE learner_id=$1 AND source_course_id=$2`, learner, l.CourseID); err != nil {
		t.Fatal(err)
	}
	edited, err := s.SaveReview(t.Context(), learner, l.CourseID, 3, "Edited private-review-text")
	if err != nil || edited.ID != saved.ID || edited.Version != 1 {
		t.Fatalf("edit=%+v %v", edited, err)
	}
	assertReviewAggregate(t, f, l.CourseID, 3, 1)
	for _, tc := range []struct{ text, verdict string }{{"abusive-review private-review-text", "block"}, {"borderline private-review-text", "needs_review"}, {"force-malformed", "pending_screening"}, {"force-deadline", "pending_screening"}, {"force-usage-missing", "pending_screening"}} {
		r, err := s.SaveReview(t.Context(), learner, l.CourseID, 4, tc.text)
		if err != nil || r.Verdict != tc.verdict || r.Text != "" {
			t.Fatalf("screen %s=%+v %v", tc.text, r, err)
		}
		p, err := s.Reviews(t.Context(), learner, l.CourseID)
		if err != nil || len(p.Items) != 1 || p.Items[0].Text != "" || p.Mine == nil {
			t.Fatalf("hidden page=%+v %v", p, err)
		}
		assertReviewAggregate(t, f, l.CourseID, 4, 1)
	}
	if _, err = s.SaveReview(t.Context(), o, l.CourseID, 5, "Self"); !errors.Is(err, sharing.ErrSelfReview) {
		t.Fatalf("self review=%v", err)
	}
	// Concurrent edits preserve one entry and exactly one contribution.
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.SaveReview(t.Context(), learner, l.CourseID, 2, ""); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, sharing.ErrCourseUnavailable) {
			t.Fatal(err)
		}
	}
	assertReviewAggregate(t, f, l.CourseID, 2, 1)
	if err = s.DeleteReview(t.Context(), learner, l.CourseID); err != nil {
		t.Fatal(err)
	}
	assertReviewAggregate(t, f, l.CourseID, 0, 0)
	// The 20/day cap counts edits and survives delete/recreate.
	used := count(t, f, `SELECT count(*) FROM course_review_submissions WHERE learner_id=$1`, learner)
	for i := used; i < 20; i++ {
		if _, err = s.SaveReview(t.Context(), learner, l.CourseID, 5, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.SaveReview(t.Context(), learner, l.CourseID, 5, ""); !errors.Is(err, sharing.ErrRateLimited) {
		t.Fatalf("daily cap=%v", err)
	}
	// Opt-out sends no review or classifier events.
	other := newLearner(t, f, "reviews-optout")
	consent(t, f, other, true)
	if _, err = s.EnrollMarketplace(t.Context(), other, l.CourseID); err != nil {
		t.Fatal(err)
	}
	completeReviewLessons(t, f, other, l.CourseID)
	before := len(ev.snapshot())
	if _, err = s.SaveReview(t.Context(), other, l.CourseID, 1, "Optout private-review-text"); err != nil {
		t.Fatal(err)
	}
	if len(ev.snapshot()) != before {
		t.Fatal("optout emitted analytics")
	}
	assertReviewAggregate(t, f, l.CourseID, 6, 2)
	// Database FK deletion also erases text and attribution without changing stars.
	if _, err = f.DB.Pool.Exec(t.Context(), `DELETE FROM learners WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	assertReviewAggregate(t, f, l.CourseID, 6, 2)
	if n := count(t, f, `SELECT count(*) FROM course_reviews WHERE course_id=$1 AND learner_id IS NULL AND text='' AND stars=1`, l.CourseID); n != 1 {
		t.Fatalf("erased reviews=%d", n)
	}
	if len(ev.named("review_submitted")) == 0 || len(ev.named("review_deleted")) != 1 || len(ev.named("$ai_generation")) == 0 {
		t.Fatalf("missing review/AI delivery: %+v", ev.snapshot())
	}
	unknownUsage := false
	for _, event := range ev.named("$ai_generation") {
		if event["error_code"] == "screening_unavailable" && event["usage_available"] == false {
			unknownUsage = true
		}
	}
	if !unknownUsage {
		t.Fatal("transport failure did not report unknown screening usage")
	}
	raw, _ := json.Marshal(ev.snapshot())
	if strings.Contains(string(raw), "private-review-text") {
		t.Fatal("review text in events")
	}
	for _, e := range ev.named("review_submitted") {
		if e["_id"] != learner || e["source"] != "api" || e["platform"] != "web" || e["course_id"] != l.CourseID {
			t.Fatalf("event common/identity=%+v", e)
		}
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `UPDATE course_review_screenings SET created_at=now()-interval '2 years'`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `UPDATE course_review_submissions SET created_at=now()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PurgeExpired(t.Context()); err != nil || n == 0 {
		t.Fatalf("review retention purge=%d %v", n, err)
	}
	if n := count(t, f, `SELECT (SELECT count(*) FROM course_review_screenings)+(SELECT count(*) FROM course_review_submissions)`); n != 0 {
		t.Fatalf("expired metadata rows=%d", n)
	}
	assertReviewAggregate(t, f, l.CourseID, 6, 2)

}

func completeReviewLessons(t *testing.T, f *backuptest.Fixture, actor, course string) {
	t.Helper()
	_, err := f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_completion_outbox(learner_id,course_id,lesson_id)
 SELECT cc.learner_id,cc.copy_course_id,st.id FROM course_copies cc JOIN course_units u ON u.course_id=cc.copy_course_id JOIN course_lesson_stubs st ON st.unit_id=u.id
 WHERE cc.learner_id=$1 AND cc.source_course_id=$2 ORDER BY st.position LIMIT 3 ON CONFLICT DO NOTHING`, actor, course)
	if err != nil {
		t.Fatal(err)
	}
}
func assertReviewAggregate(t *testing.T, f *backuptest.Fixture, course string, sum, count int) {
	t.Helper()
	var gotSum, gotCount int
	if err := f.DB.Pool.QueryRow(t.Context(), `SELECT rating_sum,rating_count FROM marketplace_listing_stats WHERE course_id=$1`, course).Scan(&gotSum, &gotCount); err != nil || gotSum != sum || gotCount != count {
		t.Fatalf("aggregate=(%d,%d), want (%d,%d): %v", gotSum, gotCount, sum, count, err)
	}
}

func (classifier) ScreenReview(ctx context.Context, r *pb.ScreenReviewRequest) (*pb.ScreenReviewResponse, error) {
	if r.Limits == nil || r.Limits.TimeoutMs > 10000 || r.Limits.MaxAttempts != 2 || r.Limits.InputTokens > 4000 || r.Limits.MaxCostNanos > 20000000 || r.Limits.DeadlineUnixMs == 0 {
		return nil, errors.New("unbounded request")
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
		return nil, errors.New("missing grpc deadline")
	}
	v := sharing.VerdictPass
	codes := []string{}
	switch r.Text {
	case "force-deadline":
		return nil, context.DeadlineExceeded
	case "force-malformed":
		v = "maybe"
	default:
		if strings.Contains(r.Text, "abusive-review") {
			v = sharing.VerdictBlock
			codes = []string{"harmful_content"}
		}
		if strings.Contains(r.Text, "borderline") {
			v = sharing.VerdictReview
			codes = []string{"harmful_content"}
		}
	}
	return &pb.ScreenReviewResponse{Verdict: v, ReasonCodes: codes, PolicyVersion: "review-v1", Attempts: []*pb.Usage{{Provider: "fixture", Model: "fixture", Attempt: 1, Delivered: true, InputTokens: 500, OutputTokens: 20, LatencyMs: 10, UsageUnavailable: r.Text == "force-usage-missing"}}}, nil
}

func (c rpcScreener) ScreenReview(ctx context.Context, r *pb.ScreenReviewRequest) (*pb.ScreenReviewResponse, error) {
	return c.EngineServiceClient.ScreenReview(ctx, r)
}

// A provider may finish after a concurrent delete. Its old verdict must never restore content.
type heldReviewScreener struct {
	started chan struct{}
	release chan struct{}
}

func (h heldReviewScreener) ScreenReview(ctx context.Context, r *pb.ScreenReviewRequest) (*pb.ScreenReviewResponse, error) {
	close(h.started)
	select {
	case <-h.release:
		return classifier{}.ScreenReview(ctx, r)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func TestReviewScreeningLateResponseCannotRestoreDeletedText(t *testing.T) {
	f := backuptest.New(t)
	s := marketService(t, f, &capture{}, nil, nil)
	o := marketOwner(t, f, "late-review-owner")
	l := listCourse(t, s, o, marketCourse(t, f, o, "Late screening", "Description", 3), "tech", "en")
	actor := newLearner(t, f, "late-reviewer")
	if _, err := s.EnrollMarketplace(t.Context(), actor, l.CourseID); err != nil {
		t.Fatal(err)
	}
	completeReviewLessons(t, f, actor, l.CourseID)
	held := heldReviewScreener{started: make(chan struct{}), release: make(chan struct{})}
	s.ConfigureReviewScreening(held)
	done := make(chan error, 1)
	go func() { _, err := s.SaveReview(t.Context(), actor, l.CourseID, 5, "Private pending text"); done <- err }()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(held.release) }) }
	defer release()
	select {
	case <-held.started:
	case err := <-done:
		t.Fatalf("save failed before screening: %v", err)
	}
	p, err := s.Reviews(t.Context(), actor, l.CourseID)
	if err != nil || p.Mine == nil || p.Mine.Text != "" || p.Mine.Verdict != "pending_screening" {
		t.Fatalf("pending text was visible: %+v %v", p, err)
	}
	if err = s.DeleteReview(t.Context(), actor, l.CourseID); err != nil {
		t.Fatal(err)
	}
	release()
	if err = <-done; !errors.Is(err, sharing.ErrCourseUnavailable) {
		t.Fatalf("stale response=%v", err)
	}
	assertReviewAggregate(t, f, l.CourseID, 0, 0)
	if n := count(t, f, `SELECT count(*) FROM course_reviews WHERE course_id=$1`, l.CourseID); n != 0 {
		t.Fatal("late response restored deleted review")
	}
}
