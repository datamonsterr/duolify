//go:build integration

package sharing_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

const reportText = "private-report-text copied chapter"

func reporters(t *testing.T, f *backuptest.Fixture, n int) []string {
	t.Helper()
	out := make([]string, n)
	for i := range out {
		out[i] = newLearner(t, f, "Reporter")
	}
	return out
}

func claimUsername(t *testing.T, f *backuptest.Fixture, id, username string) {
	t.Helper()
	if _, err := f.DB.Pool.Exec(t.Context(), `INSERT INTO learner_usernames(learner_id,username) VALUES($1,$2)`, id, username); err != nil {
		t.Fatal(err)
	}
}

func TestReportTargetsAndHideReportedCourses(t *testing.T) {
	f := newSharingFixture(t)
	r := startRedis(t)
	ev := &capture{}
	s := marketService(t, f, ev, r, nil)
	owner := marketOwner(t, f, "report-owner")
	l := listCourse(t, s, owner, marketCourse(t, f, owner, "Reported alpha rpt", "Short lessons", 3), "tech", "en")
	other := marketOwner(t, f, "report-other")
	listCourse(t, s, other, marketCourse(t, f, other, "Reported beta rpt", "Short lessons", 3), "tech", "en")
	who := reporters(t, f, 4)
	enrolled := newLearner(t, f, "Enrolled")
	if _, err := s.EnrollMarketplace(t.Context(), enrolled, l.CourseID); err != nil {
		t.Fatal(err)
	}
	q := sharing.BrowseQuery{Q: "rpt", Sort: "new"}
	titles := func() string { got, _ := browseTitles(t, s, "", q); return strings.Join(got, "|") }
	if got := titles(); !strings.Contains(got, "Reported alpha") || len(pageKeys(t, r)) < 2 {
		t.Fatalf("cache not warm: %q %v", got, pageKeys(t, r))
	}
	course := func(reason, text string) sharing.ReportInput {
		return sharing.ReportInput{TargetType: "course", TargetID: l.CourseID, Reason: reason, Text: text}
	}

	t.Run("refusals_are_neutral_and_store_nothing", func(t *testing.T) {
		for _, in := range []sharing.ReportInput{course("bogus", ""), course("spam", strings.Repeat("界", 501)), {TargetType: "thing", TargetID: l.CourseID, Reason: "spam"}, {TargetType: "course", TargetID: "not-a-uuid", Reason: "spam"}} {
			if _, err := s.Report(t.Context(), who[0], in); !errors.Is(err, sharing.ErrInvalidReport) {
				t.Fatalf("%+v = %v; want invalid_report", in, err)
			}
		}
		if _, err := s.Report(t.Context(), owner, course("spam", "")); !errors.Is(err, sharing.ErrSelfReport) {
			t.Fatalf("owner report = %v", err)
		}
		if _, err := s.Report(t.Context(), who[0], sharing.ReportInput{TargetType: "course", TargetID: "00000000-0000-4000-8000-0000000000aa", Reason: "spam"}); !errors.Is(err, sharing.ErrCourseUnavailable) {
			t.Fatalf("unknown course = %v", err)
		}
		if n := count(t, f, `SELECT count(*) FROM content_reports`); n != 0 {
			t.Fatalf("%d reports stored by refusals", n)
		}
	})

	t.Run("duplicates_merge_and_two_reporters_do_not_hide", func(t *testing.T) {
		first, err := s.Report(t.Context(), who[0], course("copyright", reportText))
		if err != nil || first.Merged {
			t.Fatalf("first = %+v %v", first, err)
		}
		again, err := s.Report(t.Context(), who[0], course("spam", ""))
		if err != nil || !again.Merged {
			t.Fatalf("duplicate = %+v %v", again, err)
		}
		if n := count(t, f, `SELECT count(*) FROM content_reports WHERE reporter_id=$1 AND status='open' AND reason='copyright' AND merge_count=1`, who[0]); n != 1 {
			t.Fatalf("merged open reports = %d", n)
		}
		if _, err = s.Report(t.Context(), who[1], course("spam", "")); err != nil {
			t.Fatal(err)
		}
		if got := titles(); !strings.Contains(got, "Reported alpha") {
			t.Fatalf("hidden at two reporters (one merged): %q", got)
		}
		if n := count(t, f, `SELECT count(*) FROM moderation_queue_items`); n != 0 {
			t.Fatalf("queue before threshold = %d", n)
		}
	})

	t.Run("third_distinct_reporter_hides_everywhere_at_once", func(t *testing.T) {
		if len(pageKeys(t, r)) < 2 {
			t.Fatal("cache must be warm before the third report")
		}
		if _, err := s.Report(t.Context(), who[2], course("unsafe", "")); err != nil {
			t.Fatal(err)
		}
		if keys := pageKeys(t, r); len(keys) != 0 {
			t.Fatalf("redis not invalidated: %v", keys)
		}
		if got := titles(); strings.Contains(got, "Reported alpha") || !strings.Contains(got, "Reported beta") {
			t.Fatalf("list after hide = %q", got)
		}
		if _, err := s.MarketCourse(t.Context(), "", l.CourseID); !errors.Is(err, sharing.ErrCourseUnavailable) {
			t.Fatalf("course page = %v", err)
		}
		if _, err := s.Reviews(t.Context(), who[3], l.CourseID); !errors.Is(err, sharing.ErrCourseUnavailable) {
			t.Fatalf("reviews = %v", err)
		}
		queue, err := s.ModerationQueue(t.Context(), 10)
		if err != nil || len(queue) != 1 || queue[0].TargetType != "course" || queue[0].TargetID != l.CourseID || queue[0].Severity != 100 || queue[0].ReportCount != 3 {
			t.Fatalf("queue = %+v %v", queue, err)
		}
		// Reports never delete or block existing copies, and relisting cannot clear the flag.
		if n := count(t, f, `SELECT count(*) FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id WHERE cc.learner_id=$1 AND cc.source_course_id=$2 AND c.deleted_at IS NULL`, enrolled, l.CourseID); n != 1 {
			t.Fatalf("copy affected: %d", n)
		}
		if _, err = s.ListCourse(t.Context(), owner, l.CourseID, sharing.ListingInput{Category: "tech", Language: "en"}); err != nil {
			t.Fatal(err)
		}
		if got := titles(); strings.Contains(got, "Reported alpha") {
			t.Fatalf("relist un-hid the course: %q", got)
		}
		// A fourth reporter still files (the course stays hidden) and raises nothing twice.
		if _, err = s.Report(t.Context(), who[3], course("spam", "")); err != nil {
			t.Fatal(err)
		}
		if n := count(t, f, `SELECT count(*) FROM moderation_queue_items WHERE status='open'`); n != 1 {
			t.Fatalf("queue rows = %d", n)
		}
	})

	t.Run("analytics_are_ids_and_enums_only", func(t *testing.T) {
		if got := len(ev.named("course_reported")); got != 4 {
			t.Fatalf("course_reported = %d; want 4 (merge is not a new report)", got)
		}
		hidden := ev.named("course_hidden_by_reports")
		if len(hidden) != 1 || hidden[0]["course_id"] != l.CourseID || hidden[0]["reporter_count"] != 3 {
			t.Fatalf("hidden events = %+v", hidden)
		}
		raw, _ := json.Marshal(ev.events)
		for _, secret := range []string{"private-report-text", "copied chapter"} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("report text leaked into analytics: %s", secret)
			}
		}
		for _, e := range ev.named("course_reported") {
			if e["target_type"] != "course" || e["course_id"] != l.CourseID {
				t.Fatalf("event = %+v", e)
			}
		}
	})

	t.Run("reporter_erasure_removes_text_and_attribution_only", func(t *testing.T) {
		if _, err := f.DB.Pool.Exec(t.Context(), `DELETE FROM learners WHERE id=$1`, who[0]); err != nil {
			t.Fatal(err)
		}
		if n := count(t, f, `SELECT count(*) FROM content_reports WHERE reporter_id IS NULL AND text='' AND reason='copyright' AND status='open'`); n != 1 {
			t.Fatalf("erased report rows = %d", n)
		}
		if n := count(t, f, `SELECT count(*) FROM marketplace_listings WHERE course_id=$1 AND hidden_at IS NOT NULL`, l.CourseID); n != 1 {
			t.Fatal("erasing a reporter un-hid the course")
		}
	})
}

func TestReportDistinctReporterRaceHidesExactlyOnce(t *testing.T) {
	f := newSharingFixture(t)
	ev := &capture{}
	s := marketService(t, f, ev, nil, nil)
	owner := marketOwner(t, f, "race-owner")
	l := listCourse(t, s, owner, marketCourse(t, f, owner, "Race course rpt", "d", 3), "tech", "en")
	who := reporters(t, f, 8)
	var wg sync.WaitGroup
	errs := make(chan error, len(who)+5)
	for _, id := range who {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Report(t.Context(), id, sharing.ReportInput{TargetType: "course", TargetID: l.CourseID, Reason: "spam"})
			errs <- err
		}()
	}
	// The same learner racing itself still leaves one open report.
	dup := newLearner(t, f, "Duplicate")
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Report(t.Context(), dup, sharing.ReportInput{TargetType: "course", TargetID: l.CourseID, Reason: "spam"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, f, `SELECT count(*) FROM content_reports WHERE target_id=$1 AND status='open'`, l.CourseID); n != 9 {
		t.Fatalf("open reports = %d; want 9", n)
	}
	if n := count(t, f, `SELECT count(*) FROM moderation_queue_items WHERE target_id=$1`, l.CourseID); n != 1 {
		t.Fatalf("queue items = %d; want exactly 1", n)
	}
	if got := len(ev.named("course_hidden_by_reports")); got != 1 {
		t.Fatalf("hidden events = %d; want exactly 1", got)
	}
	if n := count(t, f, `SELECT count(*) FROM marketplace_listings WHERE course_id=$1 AND hidden_at IS NOT NULL`, l.CourseID); n != 1 {
		t.Fatal("course not hidden")
	}
}

func TestReportRateLimitsReviewsAndUsers(t *testing.T) {
	f := newSharingFixture(t)
	ev := &capture{}
	s := marketService(t, f, ev, nil, func(c *sharing.Config) { c.ReportMaxPerDay = 3 })
	if sharing.DefaultConfig().ReportMaxPerDay != 10 || sharing.DefaultConfig().ReportHideThreshold != 3 {
		t.Fatal("defaults must be 10 per day and a threshold of 3")
	}
	owner := marketOwner(t, f, "limit-owner")
	l := listCourse(t, s, owner, marketCourse(t, f, owner, "Limit course rpt", "d", 3), "tech", "en")
	reviewer, reporter := newLearner(t, f, "Reviewer"), newLearner(t, f, "Reporter")
	claimUsername(t, f, reviewer, "ReviewerOne")
	var review string
	if err := f.DB.Pool.QueryRow(t.Context(), `INSERT INTO course_reviews(course_id,learner_id,version,stars,text,verdict) VALUES($1,$2,1,1,'Rude words','pass') RETURNING id::text`, l.CourseID, reviewer).Scan(&review); err != nil {
		t.Fatal(err)
	}
	for _, in := range []sharing.ReportInput{
		{TargetType: "review", TargetID: review, Reason: "inappropriate", Text: reportText},
		{TargetType: "user", TargetID: "reviewerone", Reason: "spam"}, // usernames are case-insensitive
	} {
		if res, err := s.Report(t.Context(), reporter, in); err != nil || res.Merged {
			t.Fatalf("%+v = %+v %v", in, res, err)
		}
	}
	if _, err := s.Report(t.Context(), reviewer, sharing.ReportInput{TargetType: "review", TargetID: review, Reason: "spam"}); !errors.Is(err, sharing.ErrSelfReport) {
		t.Fatalf("own review = %v", err)
	}
	if _, err := s.Report(t.Context(), reviewer, sharing.ReportInput{TargetType: "user", TargetID: "ReviewerOne", Reason: "spam"}); !errors.Is(err, sharing.ErrSelfReport) {
		t.Fatalf("self user = %v", err)
	}
	if _, err := s.Report(t.Context(), reporter, sharing.ReportInput{TargetType: "user", TargetID: "NobodyHere", Reason: "spam"}); !errors.Is(err, sharing.ErrCourseUnavailable) {
		t.Fatalf("unknown user = %v", err)
	}
	if _, err := s.Report(t.Context(), reporter, sharing.ReportInput{TargetType: "course", TargetID: l.CourseID, Reason: "other"}); err != nil {
		t.Fatal(err)
	}
	// The fourth submission inside 24 hours is refused, merge or not, and stores nothing.
	before := count(t, f, `SELECT count(*) FROM content_reports`)
	if _, err := s.Report(t.Context(), reporter, sharing.ReportInput{TargetType: "course", TargetID: l.CourseID, Reason: "other"}); !errors.Is(err, sharing.ErrRateLimited) {
		t.Fatalf("cap = %v", err)
	}
	if after := count(t, f, `SELECT count(*) FROM content_reports`); after != before {
		t.Fatalf("rate-limited submission stored a row: %d -> %d", before, after)
	}
	// Reviews and users never hide a course, and the reported review text is untouched.
	if n := count(t, f, `SELECT count(*) FROM moderation_queue_items`); n != 0 {
		t.Fatalf("queue = %d", n)
	}
	// Counters older than a day stop counting.
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE content_report_submissions SET created_at=now()-interval '25 hours' WHERE reporter_id=$1`, reporter); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Report(t.Context(), reporter, sharing.ReportInput{TargetType: "course", TargetID: l.CourseID, Reason: "other"}); err != nil {
		t.Fatalf("after window = %v", err)
	}
}

func TestReportRouteIsNeutralAndPrivate(t *testing.T) {
	f := newSharingFixture(t)
	s := marketService(t, f, &capture{}, nil, nil)
	owner := marketOwner(t, f, "route-owner")
	l := listCourse(t, s, owner, marketCourse(t, f, owner, "Route course rpt", "d", 3), "tech", "en")
	who := reporters(t, f, 1)[0]
	mux := http.NewServeMux()
	pass := func(h http.Handler) http.Handler { return h }
	sharing.Mount(mux, s, pass, pass)
	post := func(actor, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(accounts.WithLearnerID(t.Context(), actor), http.MethodPost, "/v1/marketplace/reports", strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	body := `{"target_type":"course","target_id":"` + l.CourseID + `","reason":"spam","text":"` + reportText + `"}`
	rec := post(who, body)
	if rec.Code != 202 || strings.TrimSpace(rec.Body.String()) != `{"status":"received","merged":false}` {
		t.Fatalf("first = %d %s", rec.Code, rec.Body)
	}
	if rec = post(who, body); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"merged":true`) {
		t.Fatalf("merge = %d %s", rec.Code, rec.Body)
	}
	for body, want := range map[string]int{
		`{"target_type":"course","target_id":"` + l.CourseID + `","reason":"nope"}`:           400,
		`{"target_type":"course","target_id":"` + l.CourseID + `","reason":"spam","extra":1}`: 400,
		`not json`: 400,
	} {
		if rec = post(who, body); rec.Code != want || strings.Contains(rec.Body.String(), l.CourseID) {
			t.Fatalf("%s = %d %s", body, rec.Code, rec.Body)
		}
	}
	if rec = post(owner, body); rec.Code != 403 || !strings.Contains(rec.Body.String(), "self_report") {
		t.Fatalf("owner = %d %s", rec.Code, rec.Body)
	}
	if rec = post("", body); rec.Code != 401 {
		t.Fatalf("anonymous = %d", rec.Code)
	}
	// The owner's review and course payloads carry no report signal at all.
	page, err := s.MarketCourse(t.Context(), owner, l.CourseID)
	raw, _ := json.Marshal(page)
	if err != nil || strings.Contains(strings.ToLower(string(raw)), "report") {
		t.Fatalf("owner payload mentions reports: %s %v", raw, err)
	}
	off := marketService(t, f, &capture{}, nil, func(c *sharing.Config) { c.MarketplaceEnabled = false })
	mux = http.NewServeMux()
	sharing.Mount(mux, off, pass, pass)
	if rec = post(who, body); rec.Code != 404 {
		t.Fatalf("flag off = %d", rec.Code)
	}
}
