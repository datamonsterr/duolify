package steering

import (
	"testing"
	"time"
)

func TestProposalConfirmationValidity(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name      string
		state     string
		decision  string
		expiresAt time.Time
		want      string
	}{
		{
			name:      "fresh proposed can confirm",
			state:     "proposed",
			decision:  "",
			expiresAt: now.Add(time.Hour),
			want:      "",
		},
		{
			name:      "expired proposed cannot confirm",
			state:     "proposed",
			decision:  "",
			expiresAt: now.Add(-time.Hour),
			want:      "proposal_expired",
		},
		{
			name:      "already confirmed is idempotent",
			state:     "proposed",
			decision:  "confirmed",
			expiresAt: now.Add(time.Hour),
			want:      "already_confirmed",
		},
		{
			name:      "discarded cannot confirm",
			state:     "proposed",
			decision:  "discarded",
			expiresAt: now.Add(time.Hour),
			want:      "proposal_discarded",
		},
		{
			name:      "pending proposal cannot confirm",
			state:     "pending",
			decision:  "",
			expiresAt: now.Add(time.Hour),
			want:      "proposal_not_ready",
		},
		{
			name:      "failed proposal cannot confirm",
			state:     "failed",
			decision:  "",
			expiresAt: now.Add(time.Hour),
			want:      "proposal_not_ready",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkCanConfirm(tc.state, tc.decision, tc.expiresAt, now)
			if got != tc.want {
				t.Fatalf("checkCanConfirm(%s, %s, %v) = %q; want %q", tc.state, tc.decision, tc.expiresAt, got, tc.want)
			}
		})
	}
}

func TestProposalDiscardValidity(t *testing.T) {
	cases := []struct {
		name     string
		state    string
		decision string
		want     string
	}{
		{
			name:     "fresh proposed can discard",
			state:    "proposed",
			decision: "",
			want:     "",
		},
		{
			name:     "already discarded is idempotent",
			state:    "proposed",
			decision: "discarded",
			want:     "already_discarded",
		},
		{
			name:     "confirmed cannot discard",
			state:    "proposed",
			decision: "confirmed",
			want:     "proposal_confirmed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkCanDiscard(tc.state, tc.decision)
			if got != tc.want {
				t.Fatalf("checkCanDiscard(%s, %s) = %q; want %q", tc.state, tc.decision, got, tc.want)
			}
		})
	}
}
