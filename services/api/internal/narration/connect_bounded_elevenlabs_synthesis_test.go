package narration

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestVendorFailureCodesAreFixedAndNeverEchoTheVendorMessage(t *testing.T) {
	secret := "vendor body: " + strings.ToUpper("sk_live_secret") + " The sky is a map." // nolint:gosec // redaction fixture, not a credential
	for _, c := range []struct {
		code      codes.Code
		want      string
		retryable bool
	}{
		{codes.ResourceExhausted, "rate_limited", true},
		{codes.Unavailable, "engine_unavailable", true},
		{codes.DeadlineExceeded, "timeout", true},
		{codes.DataLoss, "engine_unavailable", true},
	} {
		err := status.Error(c.code, secret)
		got := failureCode(err)
		if got != c.want || retryable(err) != c.retryable || strings.Contains(strings.ToLower(got), "secret") {
			t.Fatalf("%v: code=%q retryable=%v", c.code, got, retryable(err))
		}
	}
}
