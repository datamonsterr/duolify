package sharing_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func TestActionMatrixPerTargetType(t *testing.T) {
	cases := []struct {
		target string
		allow  []string
	}{
		{"course", []string{"dismiss", "warn", "delist", "takedown", "ban_publishing"}},
		{"publication", []string{"dismiss", "delist", "takedown", "ban_publishing"}},
		{"review", []string{"dismiss", "warn", "delist", "takedown"}},
		{"user", []string{"dismiss", "warn", "ban_publishing"}},
	}
	all := []string{"dismiss", "warn", "delist", "takedown", "ban_publishing"}
	for _, c := range cases {
		for _, action := range all {
			want := false
			for _, a := range c.allow {
				want = want || a == action
			}
			if got := sharing.ActionAllowed(c.target, action); got != want {
				t.Errorf("ActionAllowed(%s, %s) = %v; want %v", c.target, action, got, want)
			}
		}
		if sharing.ActionAllowed(c.target, "obliterate") || sharing.ActionAllowed(c.target, "") || sharing.ActionAllowed(c.target, "DELIST") {
			t.Errorf("%s accepts an unknown action", c.target)
		}
	}
	if sharing.ActionAllowed("lesson", "dismiss") {
		t.Error("unknown target accepted")
	}
}

func TestValidateModerationBoundaries(t *testing.T) {
	cfg := sharing.DefaultConfig()
	good := sharing.ActionInput{Action: "delist", ReasonCode: "spam", Reason: "Promotional content."}
	if err := sharing.ValidateModeration("course", good, cfg); err != nil {
		t.Fatalf("good = %v", err)
	}
	edge := good
	edge.Reason = strings.Repeat("界", cfg.ModerationReasonMaxChars)
	if err := sharing.ValidateModeration("course", edge, cfg); err != nil {
		t.Fatalf("exactly at the limit = %v", err)
	}
	for name, in := range map[string]sharing.ActionInput{
		"blank reason":   {Action: "delist", ReasonCode: "spam", Reason: " \n\t "},
		"empty reason":   {Action: "delist", ReasonCode: "spam"},
		"over the limit": {Action: "delist", ReasonCode: "spam", Reason: strings.Repeat("界", cfg.ModerationReasonMaxChars+1)},
		"nul byte":       {Action: "delist", ReasonCode: "spam", Reason: "a\x00b"},
		"bad utf8":       {Action: "delist", ReasonCode: "spam", Reason: "a\xffb"},
		"unknown code":   {Action: "delist", ReasonCode: "vibes", Reason: "x"},
		"upper code":     {Action: "delist", ReasonCode: "SPAM", Reason: "x"},
		"empty code":     {Action: "delist", Reason: "x"},
		"unknown action": {Action: "burn", ReasonCode: "spam", Reason: "x"},
	} {
		if err := sharing.ValidateModeration("course", in, cfg); !errors.Is(err, sharing.ErrInvalidModeration) {
			t.Errorf("%s = %v", name, err)
		}
	}
	if err := sharing.ValidateModeration("user", sharing.ActionInput{Action: "takedown", ReasonCode: "spam", Reason: "x"}, cfg); !errors.Is(err, sharing.ErrInvalidModeration) {
		t.Errorf("user takedown = %v", err)
	}
}

func TestSevereOnlyForTakedownWithConfiguredCodes(t *testing.T) {
	cfg := sharing.DefaultConfig()
	for _, c := range []struct {
		action, code string
		want         bool
	}{
		{"takedown", "unsafe", true}, {"takedown", "legal", true}, {"takedown", "spam", false}, {"takedown", "copyright", false},
		{"delist", "unsafe", false}, {"warn", "legal", false}, {"ban_publishing", "unsafe", false}, {"dismiss", "no_violation", false},
	} {
		if got := sharing.IsSevere(c.action, c.code, cfg); got != c.want {
			t.Errorf("IsSevere(%s,%s) = %v; want %v", c.action, c.code, got, c.want)
		}
	}
	cfg.ModerationSevereCodes = []string{"copyright"}
	if !sharing.IsSevere("takedown", "copyright", cfg) || sharing.IsSevere("takedown", "unsafe", cfg) {
		t.Error("severity must follow the configuration")
	}
}

func TestFortyEightHourTargetAndOverdue(t *testing.T) {
	cfg := sharing.DefaultConfig()
	opened := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	if got := sharing.DueAt(opened, cfg); !got.Equal(opened.Add(48 * time.Hour)) {
		t.Fatalf("due = %v", got)
	}
	for _, c := range []struct {
		age  time.Duration
		want bool
	}{{0, false}, {47*time.Hour + 59*time.Minute, false}, {48 * time.Hour, false}, {48*time.Hour + time.Second, true}, {500 * time.Hour, true}} {
		if got := sharing.IsOverdue(opened, opened.Add(c.age), cfg); got != c.want {
			t.Errorf("age %v overdue = %v; want %v", c.age, got, c.want)
		}
	}
	cfg.ModerationTargetHours = 24
	if !sharing.IsOverdue(opened, opened.Add(25*time.Hour), cfg) {
		t.Error("target must be configurable")
	}
}

func TestHeldSeverityAndConfigFromEnv(t *testing.T) {
	cfg := sharing.DefaultConfig()
	if sharing.HeldSeverity(sharing.VerdictReview, cfg) <= sharing.HeldSeverity(sharing.VerdictPending, cfg) || sharing.HeldSeverity(sharing.VerdictPending, cfg) <= 0 {
		t.Fatal("needs_review must outrank pending_screening and both must be queued")
	}
	if sharing.HeldSeverity(sharing.VerdictPass, cfg) != 0 || sharing.HeldSeverity("nonsense", cfg) != 0 {
		t.Fatal("only held verdicts have severity")
	}
	env := map[string]string{"SHARING_MODERATION_TARGET_HOURS": "24", "SHARING_MODERATION_SEVERE_CODES": "legal, bogus ,legal,unsafe", "SHARING_APPEAL_MAX_CHARS": "5000"}
	got := sharing.ConfigFromEnv(func(k string) string { return env[k] })
	if got.ModerationTargetHours != 24 || got.AppealMaxChars != 1000 || strings.Join(got.ModerationSevereCodes, ",") != "legal,unsafe" {
		t.Fatalf("env config = %+v", got)
	}
	if def := sharing.ConfigFromEnv(func(string) string { return "" }); def.ModerationTargetHours != 48 || def.MarketplaceEnabled {
		t.Fatalf("defaults = %+v", def)
	}
}

// The mux hands the handler the full pattern ("GET /v1/..."), so every route must be known to it.
// A caller without a valid moderator identity gets the neutral not_found; an unmatched pattern would
// answer unknown_route instead.
func TestModerationRoutesAreServedByTheHandler(t *testing.T) {
	routes := []string{"GET /v1/moderation/queue", "POST /v1/moderation/items/{id}/actions", "POST /v1/moderation/appeals/{id}/decision", "GET /v1/moderation/notices", "POST /v1/moderation/notices/{id}/appeal"}
	mux := http.NewServeMux()
	sharing.Mount(mux, sharing.NewService(nil, nil, sharing.DefaultConfig()), func(h http.Handler) http.Handler { return h }, func(h http.Handler) http.Handler { return h })
	for _, route := range routes {
		method, path, _ := strings.Cut(route, " ")
		body := `{"action":"dismiss","reason_code":"no_violation","reason":"ok","decision":"uphold","text":"ok"}`
		if method == "GET" {
			body = ""
		}
		req := httptest.NewRequestWithContext(accounts.WithLearnerID(t.Context(), "not-a-uuid"), method, strings.ReplaceAll(path, "{id}", "8ba637a4-18f3-4d4d-839c-37692a64630a"), strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "unknown_route") {
			t.Errorf("%s reached the handler's default case: %d %s", route, rec.Code, rec.Body)
		}
	}
}
