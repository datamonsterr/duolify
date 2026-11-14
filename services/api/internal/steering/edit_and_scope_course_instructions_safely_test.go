package steering

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestEditInputValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   EditInput
		want string
	}{
		{"missing version", EditInput{Text: ptr("x")}, "invalid_input"},
		{"nothing to change", EditInput{ExpectedVersion: ptr(uint32(1))}, "invalid_input"},
		{"bad scope", EditInput{ExpectedVersion: ptr(uint32(1)), Scope: ptr("forever")}, "invalid_scope"},
		{"500 ok", EditInput{ExpectedVersion: ptr(uint32(1)), Text: ptr(strings.Repeat("語", 500))}, ""},
		{"501 too long", EditInput{ExpectedVersion: ptr(uint32(1)), Text: ptr(strings.Repeat("語", 501))}, "too_long"},
		{"empty", EditInput{ExpectedVersion: ptr(uint32(1)), Text: ptr("  ")}, "empty"},
		{"url", EditInput{ExpectedVersion: ptr(uint32(1)), Text: ptr("see https://x.test")}, "urls_not_allowed"},
		{"injection", EditInput{ExpectedVersion: ptr(uint32(1)), Text: ptr("no quiz")}, "prompt_injection"},
		{"scope only", EditInput{ExpectedVersion: ptr(uint32(2)), Scope: ptr("rest_of_course")}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Validate(500); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestDeliveryStates(t *testing.T) {
	for _, tc := range []struct {
		scope, status string
		inRunning     bool
		jobRunning    bool
		want          string
	}{
		{"next_lesson", "active", false, false, "ready"},
		{"next_lesson", "active", true, true, "running"},
		{"next_lesson", "active", false, true, "waits"},
		{"rest_of_course", "active", true, true, "running"},
		{"rest_of_course", "active", false, true, "ready"},
		{"next_lesson", "applied", false, true, ""},
	} {
		if got := Delivery(tc.scope, tc.status, tc.inRunning, tc.jobRunning); got != tc.want {
			t.Errorf("%+v: got %q", tc, got)
		}
	}
}

func TestHTTPStatusForCodes(t *testing.T) {
	for code, want := range map[string]int{"not_found": 404, "version_conflict": 409, "not_editable": 409, "rate_limit": 429, "screening_unavailable": 503, "unavailable": 503, "too_long": 422, "invalid_input": 422} {
		if got := statusFor(code); got != want {
			t.Errorf("%s: %d", code, got)
		}
	}
}

func TestHandlerRejectsBadBodiesBeforeStorage(t *testing.T) {
	s := New(Options{})
	for _, body := range []string{`{`, `{"expected_version":1,"text":"a","extra":1}`, `{"text":"a"}`} {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/v1/courses/c/instructions/i", strings.NewReader(body))
		r.SetPathValue("id", "c")
		r.SetPathValue("instructionId", "i")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 422 || !strings.Contains(w.Body.String(), "invalid_input") {
			t.Errorf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/v1/courses/c/instructions", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Errorf("put: %d", w.Code)
	}
}
