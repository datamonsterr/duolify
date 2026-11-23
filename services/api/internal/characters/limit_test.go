package characters

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSetRetryAfterOnlyForWindowedLimits(t *testing.T) {
	w := httptest.NewRecorder()
	setRetryAfter(w, &LimitError{RetryAfter: 61500 * time.Millisecond})
	if got := w.Header().Get("Retry-After"); got != "62" {
		t.Fatalf("Retry-After=%q", got)
	}
	w = httptest.NewRecorder()
	setRetryAfter(w, &LimitError{})
	if w.Header().Get("Retry-After") != "" {
		t.Fatal("count cap must not send Retry-After")
	}
}
