package sharing_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func readyLesson(title string) sharing.SnapshotLesson {
	return sharing.SnapshotLesson{
		Title: title, Goal: "g", Generated: true,
		Slides:    []sharing.SnapshotSlide{{ID: "s1", HTML: "<h1>Hi</h1><p>Body</p>", Narration: "Say hi."}},
		Questions: []sharing.SnapshotQ{{Topic: "t", Prompt: "Which?", Choices: []string{"A", "B"}, AnswerIndex: 1, Explanation: "KEY-EXPLANATION"}},
	}
}

func TestPlanCopyMarksReadyAndNeedsGenerationExplicitly(t *testing.T) {
	missing := sharing.SnapshotLesson{Title: "Later", Goal: "g"}
	cases := []struct {
		name         string
		units        [][]sharing.SnapshotLesson
		wantStates   []string
		ready, total int
	}{
		{"first ready", [][]sharing.SnapshotLesson{{readyLesson("a"), readyLesson("b"), missing}}, []string{"current", "locked", "locked"}, 2, 3},
		{"first missing stays the E15-04 hook", [][]sharing.SnapshotLesson{{missing, readyLesson("b")}}, []string{"ready_to_generate", "locked"}, 1, 2},
		{"across units", [][]sharing.SnapshotLesson{{readyLesson("a")}, {missing}}, []string{"current", "locked"}, 1, 2},
		{"empty snapshot", nil, nil, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := sharing.Snapshot{Title: "T"}
			for _, u := range tc.units {
				snap.Units = append(snap.Units, sharing.SnapshotUnit{Title: "U", Lessons: u})
			}
			plan := sharing.PlanCopy(snap, "https://assets.example.test")
			var states []string
			for _, u := range plan.Units {
				for _, l := range u.Lessons {
					states = append(states, l.Status)
				}
			}
			if strings.Join(states, ",") != strings.Join(tc.wantStates, ",") || plan.Ready != tc.ready || plan.Total != tc.total || plan.NeedsGeneration() != tc.total-tc.ready {
				t.Fatalf("states=%v ready=%d total=%d needs=%d", states, plan.Ready, plan.Total, plan.NeedsGeneration())
			}
		})
	}
}

func TestOnlyCompleteSafeLessonsAreReusable(t *testing.T) {
	const origin = "https://assets.example.test"
	bad := map[string]func(l *sharing.SnapshotLesson){
		"no slides":           func(l *sharing.SnapshotLesson) { l.Slides = nil },
		"no questions":        func(l *sharing.SnapshotLesson) { l.Questions = nil },
		"not generated":       func(l *sharing.SnapshotLesson) { l.Generated = false },
		"answer out of range": func(l *sharing.SnapshotLesson) { l.Questions[0].AnswerIndex = 2 },
		"negative answer":     func(l *sharing.SnapshotLesson) { l.Questions[0].AnswerIndex = -1 },
		"one choice":          func(l *sharing.SnapshotLesson) { l.Questions[0].Choices = []string{"A"} },
		"blank explanation":   func(l *sharing.SnapshotLesson) { l.Questions[0].Explanation = " " },
		"blank prompt":        func(l *sharing.SnapshotLesson) { l.Questions[0].Prompt = "" },
		"network html":        func(l *sharing.SnapshotLesson) { l.Slides[0].HTML = `<img src="https://evil.test/x.png">` },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			l := readyLesson("x")
			mutate(&l)
			if sharing.Reusable(l, origin) {
				t.Fatal("must be left for generation, not reused")
			}
		})
	}
	if !sharing.Reusable(readyLesson("ok"), origin) {
		t.Fatal("a complete lesson must be reusable")
	}
}

func TestLessonObjectsNeverCarryAnswerKeys(t *testing.T) {
	const origin = "https://assets.example.test"
	l := readyLesson("Phases")
	l.RetryPool = []sharing.RetryTopic{{Topic: "t", Variants: json.RawMessage(`[{"answer_index":0}]`)}}
	key, docs, err := sharing.LessonObjects("lessons/published/draft-1/0-0", l, 8000, origin)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "lessons/published/draft-1/") || docs[key] == nil || len(docs) != 2 {
		t.Fatalf("key=%s docs=%d", key, len(docs))
	}
	for k, raw := range docs {
		for _, banned := range []string{"answer_index", "KEY-EXPLANATION", "Which?", "retry"} {
			if strings.Contains(string(raw), banned) {
				t.Fatalf("%s leaks %q", k, banned)
			}
		}
	}
	// Keys are content-addressed by the draft position, so the same input yields the same bytes.
	_, again, _ := sharing.LessonObjects("lessons/published/draft-1/0-0", l, 8000, origin)
	for k, raw := range docs {
		if string(again[k]) != string(raw) {
			t.Fatalf("%s is not deterministic", k)
		}
	}
	if _, _, err = sharing.LessonObjects("lessons/published/draft-1/0-0", l, 0, origin); err == nil {
		t.Fatal("a zero slide duration must be refused")
	}
}

func TestEnrollConfigDefaultsAndEnv(t *testing.T) {
	d := sharing.DefaultConfig()
	if d.MaxEnrollmentsPerDay != 30 || d.CopySlideMs != 8000 {
		t.Fatalf("defaults: %+v", d)
	}
	env := map[string]string{"SHARING_MAX_ENROLLMENTS_PER_DAY": "5", "SHARING_COPY_SLIDE_MS": "6000"}
	c := sharing.ConfigFromEnv(func(k string) string { return env[k] })
	if c.MaxEnrollmentsPerDay != 5 || c.CopySlideMs != 6000 {
		t.Fatalf("env: %+v", c)
	}
	env = map[string]string{"SHARING_MAX_ENROLLMENTS_PER_DAY": "-1", "SHARING_COPY_SLIDE_MS": "0"}
	c = sharing.ConfigFromEnv(func(k string) string { return env[k] })
	if c.MaxEnrollmentsPerDay != 30 || c.CopySlideMs != 8000 {
		t.Fatalf("invalid values must keep defaults: %+v", c)
	}
}

type fakeEnroller struct {
	state sharing.EnrollmentState
	got   sharing.Enrollment
	err   error
	log   []string
}

func (f *fakeEnroller) Existing(_ context.Context, viewer, token string) (sharing.EnrollmentState, error) {
	f.log = append(f.log, "existing:"+viewer+"|"+token)
	return f.state, f.err
}

func (f *fakeEnroller) Enroll(_ context.Context, viewer, token string) (sharing.Enrollment, error) {
	f.log = append(f.log, viewer+"|"+token)
	return f.got, f.err
}

func TestEnrollHandlerUsesTheSessionActorAndNeutralErrors(t *testing.T) {
	method, path := http.MethodPost, "/v1/public/shares/tok123/enroll"
	call := func(f *fakeEnroller, actor string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		h := sharing.NewEnrollHandler(f)
		mux.Handle("POST /v1/public/shares/{token}/enroll", h)
		mux.Handle("GET /v1/public/shares/{token}/enrollment", h)
		ctx := context.Background()
		if actor != "" {
			ctx = accounts.WithLearnerID(ctx, actor)
		}
		req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(`{"learner_id":"someone-else"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	f := &fakeEnroller{got: sharing.Enrollment{CourseID: "c1", Created: true, Version: 2, TotalLessons: 3, ReadyLessons: 2, NeedsGenerationLessons: 1}}
	rec := call(f, "11111111-1111-4111-8111-111111111111")
	if rec.Code != http.StatusCreated || len(f.log) != 1 || f.log[0] != "11111111-1111-4111-8111-111111111111|tok123" {
		t.Fatalf("created: %d %v", rec.Code, f.log)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["course_id"] != "c1" || body["created"] != true || body["ready_lessons"] != float64(2) || body["needs_generation_lessons"] != float64(1) {
		t.Fatalf("body: %v", body)
	}
	if strings.Contains(rec.Body.String(), "tok123") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("response must not echo the token and must not be cached")
	}
	f.got.Created = false
	if rec = call(f, "11111111-1111-4111-8111-111111111111"); rec.Code != http.StatusOK {
		t.Fatalf("existing copy: %d", rec.Code)
	}
	if rec = call(&fakeEnroller{}, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no actor: %d", rec.Code)
	}
	for err, want := range map[error]int{sharing.ErrLinkNotFound: 404, sharing.ErrRateLimited: 429, errors.New("db secret detail"): 503} {
		rec = call(&fakeEnroller{err: err}, "11111111-1111-4111-8111-111111111111")
		if rec.Code != want || strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("%v: %d %s", err, rec.Code, rec.Body.String())
		}
	}
	rec = call(&fakeEnroller{err: sharing.ErrLinkNotFound}, "11111111-1111-4111-8111-111111111111")
	if strings.TrimSpace(rec.Body.String()) != `{"error":"link_not_found"}` {
		t.Fatalf("neutral body: %s", rec.Body.String())
	}
}

func TestEnrollmentStateIsReadOnlyAndNeutral(t *testing.T) {
	const actor = "11111111-1111-4111-8111-111111111111"
	f := &fakeEnroller{state: sharing.EnrollmentState{Enrolled: true, CourseID: "c9"}}
	mux := http.NewServeMux()
	mux.Handle("GET /v1/public/shares/{token}/enrollment", sharing.NewEnrollHandler(f))
	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(accounts.WithLearnerID(context.Background(), actor), http.MethodGet, "/v1/public/shares/tok123/enrollment", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	rec := do()
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"enrolled":true,"course_id":"c9"}` {
		t.Fatalf("enrolled: %d %s", rec.Code, rec.Body.String())
	}
	f.state = sharing.EnrollmentState{}
	if rec = do(); strings.TrimSpace(rec.Body.String()) != `{"enrolled":false}` {
		t.Fatalf("not enrolled: %s", rec.Body.String())
	}
	f.err = sharing.ErrLinkNotFound
	if rec = do(); rec.Code != http.StatusNotFound {
		t.Fatalf("dead link: %d", rec.Code)
	}
}
