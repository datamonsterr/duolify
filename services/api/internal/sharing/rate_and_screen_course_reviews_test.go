package sharing_test

import (
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"strings"
	"testing"
)

func TestReviewValidation(t *testing.T) {
	for _, tc := range []struct {
		stars int
		text  string
		ok    bool
	}{{1, "", true}, {5, strings.Repeat("界", 2000), true}, {0, "", false}, {6, "", false}, {3, strings.Repeat("界", 2001), false}} {
		err := sharing.ValidateReview(tc.stars, tc.text, sharing.DefaultConfig())
		if (err == nil) != tc.ok {
			t.Errorf("stars=%d chars=%d err=%v", tc.stars, len([]rune(tc.text)), err)
		}
	}
}
