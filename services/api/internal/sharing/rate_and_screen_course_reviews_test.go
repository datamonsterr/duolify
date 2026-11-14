package sharing_test

import (
	"errors"
	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
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

func TestReviewScreeningFailsClosedOnMalformedOrUnboundedUsage(t *testing.T) {
	cfg := sharing.DefaultConfig()
	rates := sharing.Rates{Input: 1000, Output: 5000, CacheWrite: 1250, CacheRead: 100}
	good := func() *pb.ScreenReviewResponse {
		return &pb.ScreenReviewResponse{Verdict: "pass", PolicyVersion: "review-v1", Attempts: []*pb.Usage{{Attempt: 1, Delivered: true, InputTokens: 100, OutputTokens: 10, LatencyMs: 10}}}
	}
	if got := sharing.DecideReview(good(), nil, cfg, rates); got.Verdict != "pass" {
		t.Fatal(got)
	}
	for _, mutate := range []func(*pb.ScreenReviewResponse){
		func(r *pb.ScreenReviewResponse) { r.Verdict = "maybe" }, func(r *pb.ScreenReviewResponse) { r.PolicyVersion = "course-v1" },
		func(r *pb.ScreenReviewResponse) { r.ReasonCodes = []string{"harmful_content"} }, func(r *pb.ScreenReviewResponse) { r.Attempts = nil },
		func(r *pb.ScreenReviewResponse) { r.Attempts[0] = nil }, func(r *pb.ScreenReviewResponse) { r.Attempts[0].UsageUnavailable = true },
		func(r *pb.ScreenReviewResponse) { r.Attempts[0].InputTokens = 4001 }, func(r *pb.ScreenReviewResponse) { r.Attempts[0].LatencyMs = 10001 },
		func(r *pb.ScreenReviewResponse) { r.Attempts[0].FallbackUsed = true }, func(r *pb.ScreenReviewResponse) { r.Attempts[0].Attempt = 2 },
	} {
		r := good()
		mutate(r)
		if got := sharing.DecideReview(r, nil, cfg, rates); got.Verdict != "pending_screening" {
			t.Fatalf("unsafe verdict=%+v", got)
		}
	}
	if got := sharing.DecideReview(good(), errors.New("down"), cfg, rates); got.Verdict != "pending_screening" {
		t.Fatal(got)
	}
	cfg.ReviewMaxCostNanos = 1
	if got := sharing.DecideReview(good(), nil, cfg, rates); got.Verdict != "pending_screening" {
		t.Fatal(got)
	}
}
func TestReviewLimitsAreConfigured(t *testing.T) {
	cfg := sharing.ConfigFromEnv(func(k string) string {
		switch k {
		case "SHARING_REVIEW_MIN_LESSONS":
			return "4"
		case "SHARING_REVIEWS_PER_DAY":
			return "12"
		case "SHARING_REVIEW_MAX_CHARS":
			return "1000"
		case "SHARING_REVIEW_MAX_COST_NANOS":
			return "10000000"
		}
		return ""
	})
	if cfg.ReviewMinLessons != 4 || cfg.ReviewMaxPerDay != 12 || cfg.ReviewMaxChars != 1000 || cfg.ReviewMaxCostNanos != 10000000 {
		t.Fatal(cfg)
	}
}
