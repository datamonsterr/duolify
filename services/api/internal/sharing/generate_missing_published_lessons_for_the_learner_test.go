package sharing_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func TestClassifyCopyReportsReadyTotalAndTheGate(t *testing.T) {
	lesson := func(id, status string, content bool) sharing.CopyLesson {
		return sharing.CopyLesson{ID: id, Status: status, HasContent: content}
	}
	cases := []struct {
		name         string
		in           []sharing.CopyLesson
		ready, total int
		state, next  string
		wantMissing  int
	}{
		{"nothing missing", []sharing.CopyLesson{lesson("a", "done", true), lesson("b", "current", true)}, 2, 2, sharing.StateComplete, "", 0},
		{"end of published content, previous passed", []sharing.CopyLesson{lesson("a", "done", true), lesson("b", "done", true), lesson("c", "ready_to_generate", false)}, 2, 3, sharing.StateReady, "c", 1},
		{"previous not passed", []sharing.CopyLesson{lesson("a", "current", true), lesson("b", "locked", true), lesson("c", "locked", false)}, 2, 3, sharing.StateLocked, "c", 1},
		{"already generating", []sharing.CopyLesson{lesson("a", "done", true), lesson("c", "generating", false)}, 1, 2, sharing.StateGenerating, "c", 1},
		{"first lesson missing is ready at once", []sharing.CopyLesson{lesson("a", "ready_to_generate", false), lesson("b", "locked", true)}, 1, 2, sharing.StateReady, "a", 1},
		{"next is the first missing in order", []sharing.CopyLesson{lesson("a", "done", true), lesson("b", "ready_to_generate", false), lesson("c", "locked", false)}, 1, 3, sharing.StateReady, "b", 2},
		{"empty copy is complete", nil, 0, 0, sharing.StateComplete, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sharing.ClassifyCopy(tc.in)
			if got.Ready != tc.ready || got.Total != tc.total || got.State != tc.state || got.NextLessonID != tc.next || got.Missing != tc.wantMissing {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestCheckQuoteRequiresAnUnexpiredQuoteAtTheActiveVersion(t *testing.T) {
	good := pricing.Quote{Action: "lesson_content", PricingVersion: "v7"}
	cases := []struct {
		name   string
		mutate func(q *pricing.Quote)
		active string
		stale  bool
	}{
		{"valid", func(*pricing.Quote) {}, "v7", false},
		{"expired", func(q *pricing.Quote) { q.Expired = true }, "v7", true},
		{"other action", func(q *pricing.Quote) { q.Action = "tutor_short" }, "v7", true},
		{"superseded version", func(*pricing.Quote) {}, "v8", true},
		{"no active version", func(*pricing.Quote) {}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := good
			tc.mutate(&q)
			err := sharing.CheckQuote(q, tc.active)
			if tc.stale != errors.Is(err, sharing.ErrQuoteStale) || (!tc.stale && err != nil) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestBuildMissingQuoteSumsComponentsAndComparesTheBalance(t *testing.T) {
	q := pricing.Quote{ID: "q1", PricingVersion: "v7", ContentCeiling: 140, AudioCeiling: 50}
	enough := sharing.BuildMissingQuote(q, "lesson-3", 190)
	if enough.TotalCredits != 190 || enough.ContentCredits != 140 || enough.AudioCredits != 50 || !enough.Sufficient || enough.PricingVersion != "v7" || enough.QuoteID != "q1" || enough.LessonID != "lesson-3" {
		t.Fatalf("%+v", enough)
	}
	if short := sharing.BuildMissingQuote(q, "lesson-3", 189); short.Sufficient || short.Balance != 189 {
		t.Fatalf("%+v", short)
	}
}

type fakeGenerator struct {
	actor, course string
	input         sharing.GenerateMissingInput
	err           error
}

func (f *fakeGenerator) MissingStatus(_ context.Context, actor, course string) (sharing.MissingStatus, error) {
	f.actor, f.course = actor, course
	return sharing.MissingStatus{CourseID: course, Ready: 2, Total: 3, Missing: 1, State: sharing.StateReady, NextLessonID: "l3"}, f.err
}

func (f *fakeGenerator) QuoteMissing(_ context.Context, actor, course string) (sharing.MissingQuote, error) {
	f.actor, f.course = actor, course
	return sharing.MissingQuote{QuoteID: "q1", LessonID: "l3", PricingVersion: "v7", TotalCredits: 190}, f.err
}

func (f *fakeGenerator) GenerateMissing(_ context.Context, actor, course string, in sharing.GenerateMissingInput) (sharing.Generation, error) {
	f.actor, f.course, f.input = actor, course, in
	return sharing.Generation{ID: "g1", State: "queued"}, f.err
}

func missingRequest(t *testing.T, h http.Handler, actor, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	for _, p := range []string{"GET /v1/courses/{id}/missing-lessons", "POST /v1/courses/{id}/missing-lessons/quote", "POST /v1/courses/{id}/missing-lessons/generate"} {
		mux.Handle(p, h)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if actor != "" {
		req = req.WithContext(accounts.WithLearnerID(req.Context(), actor))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestMissingHandlerUsesTheSessionActorAndNeutralErrors(t *testing.T) {
	const course = "c0000000-0000-4000-8000-000000000001"
	base := "/v1/courses/" + course + "/missing-lessons"
	g := &fakeGenerator{}
	h := sharing.NewMissingHandler(g)

	if rec := missingRequest(t, h, "", http.MethodGet, base, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	if rec := missingRequest(t, h, "me", http.MethodGet, base, ""); rec.Code != http.StatusOK || g.actor != "me" || g.course != course || !strings.Contains(rec.Body.String(), `"ready":2`) {
		t.Fatalf("status: %d %s actor=%s", rec.Code, rec.Body, g.actor)
	}
	if rec := missingRequest(t, h, "me", http.MethodPost, base+"/quote", `{}`); rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"pricing_version":"v7"`) {
		t.Fatalf("quote: %d %s", rec.Code, rec.Body)
	}
	body := `{"lesson_id":"l3","quote_id":"q1","consent":true}`
	if rec := missingRequest(t, h, "me", http.MethodPost, base+"/generate", body); rec.Code != http.StatusAccepted || g.input != (sharing.GenerateMissingInput{LessonID: "l3", QuoteID: "q1", Consent: true}) || !strings.Contains(rec.Body.String(), `"generation_id":"g1"`) {
		t.Fatalf("generate: %d %s %+v", rec.Code, rec.Body, g.input)
	}
	// The actor never comes from the body.
	if rec := missingRequest(t, h, "me", http.MethodPost, base+"/generate", `{"lesson_id":"l3","quote_id":"q1","consent":true,"learner_id":"someone-else"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Fatalf("unknown field: %d %s", rec.Code, rec.Body)
	}
	if rec := missingRequest(t, h, "me", http.MethodPost, base+"/generate", `not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", rec.Code)
	}

	errs := []struct {
		err  error
		code int
		want string
	}{
		{sharing.ErrCopyNotFound, http.StatusNotFound, "copy_not_found"},
		{sharing.ErrConsentRequired, http.StatusBadRequest, "consent_required"},
		{sharing.ErrQuoteStale, http.StatusConflict, "quote_stale"},
		{sharing.ErrPreviousNotPassed, http.StatusConflict, "previous_lesson_not_passed"},
		{sharing.ErrNothingToGenerate, http.StatusConflict, "nothing_to_generate"},
		{sharing.ErrGenerationInProgress, http.StatusConflict, "generation_in_progress"},
		{sharing.ErrInsufficientCredits, http.StatusPaymentRequired, "insufficient_credits"},
		{errors.New("pq: relation secret_table does not exist"), http.StatusServiceUnavailable, "sharing_unavailable"},
	}
	for _, tc := range errs {
		g.err = tc.err
		rec := missingRequest(t, h, "me", http.MethodPost, base+"/generate", body)
		if rec.Code != tc.code || strings.TrimSpace(rec.Body.String()) != `{"error":"`+tc.want+`"}` {
			t.Errorf("%v -> %d %s", tc.err, rec.Code, rec.Body)
		}
	}
}
