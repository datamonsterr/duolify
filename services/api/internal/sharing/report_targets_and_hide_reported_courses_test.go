package sharing_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

const aCourse = "8ba637a4-18f3-4d4d-839c-37692a64630a"

func TestValidateReportBoundaries(t *testing.T) {
	cfg := sharing.DefaultConfig()
	ok := func(in sharing.ReportInput) bool { return sharing.ValidateReport(in, cfg) == nil }
	base := sharing.ReportInput{TargetType: "course", TargetID: aCourse, Reason: "spam"}
	if !ok(base) {
		t.Fatal("base report must be valid")
	}
	for name, mutate := range map[string]func(*sharing.ReportInput){
		"unknown reason":   func(in *sharing.ReportInput) { in.Reason = "rude" },
		"empty reason":     func(in *sharing.ReportInput) { in.Reason = "" },
		"upper reason":     func(in *sharing.ReportInput) { in.Reason = "SPAM" },
		"unknown target":   func(in *sharing.ReportInput) { in.TargetType = "lesson" },
		"course not uuid":  func(in *sharing.ReportInput) { in.TargetID = "123" },
		"review not uuid":  func(in *sharing.ReportInput) { in.TargetType, in.TargetID = "review", "x" },
		"user bad name":    func(in *sharing.ReportInput) { in.TargetType, in.TargetID = "user", "a b" },
		"user too short":   func(in *sharing.ReportInput) { in.TargetType, in.TargetID = "user", "ab" },
		"text too long":    func(in *sharing.ReportInput) { in.Text = strings.Repeat("界", 501) },
		"text nul":         func(in *sharing.ReportInput) { in.Text = "a\x00b" },
		"text bad utf8":    func(in *sharing.ReportInput) { in.Text = "a\xffb" },
		"uppercase course": func(in *sharing.ReportInput) { in.TargetID = strings.ToUpper(aCourse) + "0" },
	} {
		in := base
		mutate(&in)
		if ok(in) {
			t.Errorf("%s must be invalid: %+v", name, in)
		}
	}
	for name, in := range map[string]sharing.ReportInput{
		"500 unicode chars": {TargetType: "course", TargetID: aCourse, Reason: "other", Text: strings.Repeat("界", 500)},
		"user by username":  {TargetType: "user", TargetID: "Mina.K_1", Reason: "unsafe"},
		"review by id":      {TargetType: "review", TargetID: aCourse, Reason: "misleading", Text: "Rude"},
	} {
		if !ok(in) {
			t.Errorf("%s must be valid", name)
		}
	}
}

func TestReportPolicyIsConfigurable(t *testing.T) {
	cfg := sharing.DefaultConfig()
	if !slices.Equal(cfg.ReportReasons, []string{"unsafe", "inappropriate", "spam", "copyright", "misleading", "other"}) {
		t.Fatalf("reasons = %v", cfg.ReportReasons)
	}
	if cfg.ReportMaxPerDay != 10 || cfg.ReportHideThreshold != 3 || cfg.ReportMaxChars != 500 || cfg.ReportRetentionDays != 1095 {
		t.Fatalf("defaults = %+v", cfg)
	}
	env := map[string]string{"SHARING_REPORT_REASONS": "spam, unsafe,spam,bogus", "SHARING_REPORTS_PER_DAY": "4", "SHARING_REPORT_HIDE_THRESHOLD": "5", "SHARING_REPORT_MAX_CHARS": "900", "SHARING_REPORT_RETENTION_DAYS": "10"}
	got := sharing.ConfigFromEnv(func(k string) string { return env[k] })
	if !slices.Equal(got.ReportReasons, []string{"spam", "unsafe"}) || got.ReportMaxPerDay != 4 || got.ReportHideThreshold != 5 {
		t.Fatalf("env = %+v", got)
	}
	if got.ReportMaxChars != 500 || got.ReportRetentionDays != 1095 {
		t.Fatalf("out-of-range values must keep defaults: %+v", got)
	}
	if err := sharing.ValidateReport(sharing.ReportInput{TargetType: "course", TargetID: aCourse, Reason: "copyright"}, got); err == nil {
		t.Fatal("a reason removed by configuration must be refused")
	}
	if kept := sharing.ConfigFromEnv(func(k string) string {
		return map[string]string{"SHARING_REPORT_REASONS": "bogus,nope"}[k]
	}); len(kept.ReportReasons) != 6 {
		t.Fatalf("an all-unknown list must keep the defaults: %v", kept.ReportReasons)
	}
}

func TestQueueSeverityAndHideDecision(t *testing.T) {
	cfg := sharing.DefaultConfig()
	for _, tc := range []struct {
		reasons []string
		want    int
	}{{nil, 0}, {[]string{"spam"}, 30}, {[]string{"spam", "unsafe", "other"}, 100}, {[]string{"copyright", "misleading"}, 80}, {[]string{"unknown"}, 0}} {
		if got := sharing.QueueSeverity(tc.reasons, cfg); got != tc.want {
			t.Errorf("QueueSeverity(%v) = %d; want %d", tc.reasons, got, tc.want)
		}
	}
	for distinct, want := range map[int]bool{0: false, 1: false, 2: false, 3: true, 4: true} {
		if got := sharing.ShouldHide(distinct, cfg); got != want {
			t.Errorf("ShouldHide(%d) = %v", distinct, got)
		}
	}
}
