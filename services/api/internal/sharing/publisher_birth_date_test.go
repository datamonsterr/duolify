package sharing_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

type ageErrorLister struct {
	sharing.Lister
	err error
}

func (ageErrorLister) MarketplaceEnabled() bool { return true }
func (l ageErrorLister) ListCourse(context.Context, string, string, sharing.ListingInput) (sharing.Listing, error) {
	return sharing.Listing{}, l.err
}

func TestPublisherAgeErrorsRemainTypedForPublicListing(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{sharing.ErrBirthDateRequired, 409, "birth_date_required"},
		{sharing.ErrAdultRequired, 403, "adult_required"},
	} {
		r := httptest.NewRequestWithContext(t.Context(), "PUT", "/v1/courses/course/marketplace-listing", strings.NewReader(`{}`))
		r = r.WithContext(accounts.WithLearnerID(r.Context(), "a1111111-1111-4111-8111-111111111111"))
		w := httptest.NewRecorder()
		sharing.NewListingHandler(ageErrorLister{err: tc.err}).ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
			t.Fatalf("publisher error=%d %s", w.Code, w.Body)
		}
	}
}

func TestPublisherAgeConfigCannotLowerAdultRequirement(t *testing.T) {
	if got := sharing.ConfigFromEnv(func(name string) string {
		if name == "SHARING_MIN_PUBLISHER_AGE" {
			return "16"
		}
		return ""
	}).MinPublisherAge; got != 18 {
		t.Fatalf("publisher minimum=%d", got)
	}
}
