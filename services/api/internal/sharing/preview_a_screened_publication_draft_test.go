package sharing_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func lesson(title string, slides ...string) sharing.SnapshotLesson {
	l := sharing.SnapshotLesson{Title: title, Goal: "goal", Generated: len(slides) > 0}
	for i, h := range slides {
		l.Slides = append(l.Slides, sharing.SnapshotSlide{ID: "s" + string(rune('1'+i)), HTML: h, Narration: "n"})
	}
	return l
}

func TestAllowlistRejectsPrivateFields(t *testing.T) {
	for _, key := range []string{"profile", "notes", "chat", "answers", "progress", "instructions", "learner_id", "email"} {
		raw := []byte(`{"title":"t","units":[{"title":"u","lessons":[{"title":"l","` + key + `":"x"}]}]}`)
		if err := sharing.CheckAllowlist(raw); !errors.Is(err, sharing.ErrOutOfAllowlist) {
			t.Errorf("key %q accepted: %v", key, err)
		}
	}
	good, _ := json.Marshal(sharing.Snapshot{Title: "t", Units: []sharing.SnapshotUnit{{Title: "u", Lessons: []sharing.SnapshotLesson{lesson("l", "<p>x</p>")}}}})
	if err := sharing.CheckAllowlist(good); err != nil {
		t.Errorf("allowlisted snapshot rejected: %v", err)
	}
	nested := []byte(`{"title":"t","units":[{"title":"u","lessons":[{"title":"l","retry_pool":[{"topic":"a","variants":[{"prompt":"p","learner_answer":"x"}]}]}]}]}`)
	if err := sharing.CheckAllowlist(nested); !errors.Is(err, sharing.ErrOutOfAllowlist) {
		t.Errorf("nested learner answer accepted: %v", err)
	}
}

func TestScannerFlagsPIIAndKeysWithoutText(t *testing.T) {
	s := sharing.Snapshot{Title: "t", Units: []sharing.SnapshotUnit{{Title: "u", Lessons: []sharing.SnapshotLesson{
		lesson("one", "<p>fine</p>"),
		lesson("two", "<p>ok</p>", "<p>mail me at jane.doe@example.com or +1 415 555 0132</p>", "<p>key sk-live-AbCdEf0123456789AbCdEf0123456789</p>"),
	}}}}
	got := sharing.ScanFindings(s)
	want := map[string]string{KindLoc(sharing.KindPersonalData, "lesson 2 · slide 2"): "", KindLoc(sharing.KindSecret, "lesson 2 · slide 3"): ""}
	if len(got) != 2 {
		t.Fatalf("findings=%v", got)
	}
	for _, f := range got {
		delete(want, KindLoc(f.Kind, f.Location))
		if strings.Contains(f.Location, "@") || strings.Contains(f.Location, "sk-") {
			t.Errorf("finding leaks text: %v", f)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing findings %v in %v", want, got)
	}
}

func KindLoc(k, l string) string { return k + "|" + l }

func TestSampleIsBounded(t *testing.T) {
	var lessons []sharing.SnapshotLesson
	for i := 0; i < 30; i++ {
		lessons = append(lessons, lesson("l", strings.Repeat("x", 20000)))
	}
	s := sharing.Snapshot{Title: "t", Units: []sharing.SnapshotUnit{{Title: "u", Lessons: lessons}}}
	cfg := sharing.DefaultConfig()
	sample, n := sharing.BuildSample(s, cfg)
	if n != 12 || len(sample) == 0 || len(sample) > cfg.SampleMaxBytes+4096 {
		t.Fatalf("lessons=%d bytes=%d", n, len(sample))
	}
	if !json.Valid([]byte(sample)) {
		t.Fatal("sample is not valid JSON")
	}
}

func usage(attempt uint32, delivered, fallback bool) *pb.Usage {
	return &pb.Usage{Attempt: attempt, Delivered: delivered, FallbackUsed: fallback, InputTokens: 1000, OutputTokens: 20, LatencyMs: 900}
}

func TestDecideFailsClosed(t *testing.T) {
	cfg := sharing.DefaultConfig()
	rates := sharing.Rates{Input: 1000, Output: 5000}
	ok := &pb.ScreenCourseResponse{Verdict: "pass", PolicyVersion: "course-v1", Attempts: []*pb.Usage{usage(1, true, false)}}
	if got := sharing.Decide(nil, ok, nil, cfg, rates); got.Verdict != sharing.VerdictPass || got.Attempts != 1 || got.CostNanos != 1000*1000+20*5000 {
		t.Errorf("pass=%+v", got)
	}
	cases := map[string]struct {
		r   *pb.ScreenCourseResponse
		err error
	}{
		"rpc error":         {nil, errors.New("down")},
		"nil response":      {nil, nil},
		"bad verdict":       {&pb.ScreenCourseResponse{Verdict: "maybe", PolicyVersion: "course-v1", Attempts: []*pb.Usage{usage(1, true, false)}}, nil},
		"wrong policy":      {&pb.ScreenCourseResponse{Verdict: "pass", PolicyVersion: "x", Attempts: []*pb.Usage{usage(1, true, false)}}, nil},
		"no attempts":       {&pb.ScreenCourseResponse{Verdict: "pass", PolicyVersion: "course-v1"}, nil},
		"undelivered":       {&pb.ScreenCourseResponse{Verdict: "pass", PolicyVersion: "course-v1", Attempts: []*pb.Usage{usage(1, false, false)}}, nil},
		"three attempts":    {&pb.ScreenCourseResponse{Verdict: "pass", PolicyVersion: "course-v1", Attempts: []*pb.Usage{usage(1, false, false), usage(2, false, true), usage(3, true, true)}}, nil},
		"tokens over":       {&pb.ScreenCourseResponse{Verdict: "pass", PolicyVersion: "course-v1", Attempts: []*pb.Usage{{Attempt: 1, Delivered: true, InputTokens: 60001}}}, nil},
		"over latency":      {&pb.ScreenCourseResponse{Verdict: "pass", PolicyVersion: "course-v1", Attempts: []*pb.Usage{{Attempt: 1, Delivered: true, InputTokens: 10, LatencyMs: 120001}}}, nil},
		"pass with codes":   {&pb.ScreenCourseResponse{Verdict: "pass", ReasonCodes: []string{"secret"}, PolicyVersion: "course-v1", Attempts: []*pb.Usage{usage(1, true, false)}}, nil},
		"unknown code":      {&pb.ScreenCourseResponse{Verdict: "block", ReasonCodes: []string{"free text"}, PolicyVersion: "course-v1", Attempts: []*pb.Usage{usage(1, true, false)}}, nil},
		"usage unavailable": {&pb.ScreenCourseResponse{Verdict: "pass", PolicyVersion: "course-v1", Attempts: []*pb.Usage{{Attempt: 1, Delivered: true, UsageUnavailable: true}}}, nil},
	}
	for name, c := range cases {
		if got := sharing.Decide(nil, c.r, c.err, cfg, rates); got.Verdict != sharing.VerdictPending {
			t.Errorf("%s: verdict=%q want pending_screening", name, got.Verdict)
		}
	}
	capped := sharing.DefaultConfig()
	capped.MaxCostNanos = 10
	if got := sharing.Decide(nil, ok, nil, capped, rates); got.Verdict != sharing.VerdictPending {
		t.Errorf("USD cap breach verdict=%q", got.Verdict)
	}
}

func TestDecideCombinesScannerAndFallback(t *testing.T) {
	cfg, rates := sharing.DefaultConfig(), sharing.Rates{Input: 1, Output: 1}
	findings := []sharing.Finding{{Kind: sharing.KindSecret, Location: "lesson 1 · slide 1"}}
	pass := &pb.ScreenCourseResponse{Verdict: "pass", PolicyVersion: "course-v1", Attempts: []*pb.Usage{usage(1, false, false), usage(2, true, true)}}
	got := sharing.Decide(findings, pass, nil, cfg, rates)
	if got.Verdict != sharing.VerdictReview || !got.FallbackUsed || got.Attempts != 2 || !strings.Contains(strings.Join(got.ReasonCodes, ","), "secret") {
		t.Errorf("scanner finding must hold a model pass for review: %+v", got)
	}
	block := &pb.ScreenCourseResponse{Verdict: "block", ReasonCodes: []string{"prompt_injection"}, PolicyVersion: "course-v1", Attempts: []*pb.Usage{usage(1, true, false)}}
	if got := sharing.Decide(findings, block, nil, cfg, rates); got.Verdict != sharing.VerdictBlock {
		t.Errorf("block lost: %+v", got)
	}
}

func TestConfigDefaultsAreConservative(t *testing.T) {
	c := sharing.ConfigFromEnv(func(string) string { return "" })
	if c.MinPublisherAge != 18 || c.IncludeNarration || c.SampleMaxLessons != 12 || c.MaxInputTokens != 60000 || c.TimeoutMs != 120000 || c.MaxAttempts != 2 {
		t.Errorf("defaults=%+v", c)
	}
	env := map[string]string{"SHARING_MIN_PUBLISHER_AGE": "21", "SHARING_INCLUDE_NARRATION": "true"}
	c = sharing.ConfigFromEnv(func(k string) string { return env[k] })
	if c.MinPublisherAge != 21 || !c.IncludeNarration {
		t.Errorf("overlay=%+v", c)
	}
}
