package engine

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func refusal(t *testing.T, domain string) error {
	t.Helper()
	st, err := status.New(codes.Unavailable, "promotion_evidence_stale").WithDetails(&errdetails.ErrorInfo{
		Reason: "promotion_evidence_stale", Domain: domain, Metadata: map[string]string{"task": "steer_screen", "rpc": "ScreenInstruction"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return st.Err()
}

func TestNotPromotedReadsTheTypedEngineRefusal(t *testing.T) {
	reason, task, ok := NotPromoted(refusal(t, PromotionDomain))
	if !ok || reason != "promotion_evidence_stale" || task != "steer_screen" {
		t.Fatalf("got %q %q %v", reason, task, ok)
	}
	for name, err := range map[string]error{
		"nil":          nil,
		"plain":        errors.New("boom"),
		"untyped":      status.Error(codes.Unavailable, "provider_outage"),
		"other domain": refusal(t, "example.com"),
		"other code":   status.Error(codes.FailedPrecondition, "quote route differs"),
	} {
		if _, _, ok := NotPromoted(err); ok {
			t.Errorf("%s: must not read as a promotion refusal", name)
		}
	}
}

func TestRefusalIsLoggedAsAnAlertableError(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)
	logNotPromoted(context.Background(), "/duolify.engine.v1.EngineService/ScreenInstruction", refusal(t, PromotionDomain))
	logNotPromoted(context.Background(), "/x", status.Error(codes.Unavailable, "provider_outage"))
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, `"level":"ERROR"`) ||
		!strings.Contains(out, `"event":"live_feature_not_promoted"`) || !strings.Contains(out, `"reason":"promotion_evidence_stale"`) ||
		!strings.Contains(out, `"task":"steer_screen"`) {
		t.Fatalf("log: %s", out)
	}
}
