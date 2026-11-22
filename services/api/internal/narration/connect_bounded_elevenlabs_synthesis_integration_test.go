//go:build integration

package narration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *assetHarness) openReservations() int {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM credit_jobs WHERE learner_id=$1 AND state='reserved'`, owner).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

// A vendor rate limit that outlasts the engine's single retry is terminal: captions-only,
// reservation released, nothing charged, and events carry fixed codes but never text.
func TestConnectBoundedElevenLabsSynthesisIntegrationReleasesReservationOnVendorOutage(t *testing.T) {
	h := newAssetHarness(t, 30*24*time.Hour)
	faults := map[string]string{}
	for _, text := range h.texts {
		faults[text] = "ratelimit"
	}
	h.mock.SetFaults(faults)
	h.enqueue(lesson)
	if err := h.service.SynthesizeLesson(h.ctx, owner, lesson, true); err != nil {
		t.Fatal(err)
	}
	for _, slide := range []string{"sky-1", "sky-2", "sky-3"} {
		if h.status(lesson, slide) != "failed" {
			t.Fatal("slide must fall back to captions only", slide, h.status(lesson, slide))
		}
	}
	if h.spent() != 0 || h.openReservations() != 0 {
		t.Fatal("undelivered audio must be released and uncharged", h.spent(), h.openReservations())
	}
	failed := h.events.named("narration_failed")
	if len(failed) != 3 {
		t.Fatal("narration_failed events", len(failed))
	}
	for _, e := range failed {
		if e["error_code"] != "rate_limited" {
			t.Fatal("error_code", e["error_code"])
		}
	}
	generations := h.events.named("$ai_generation")
	if len(generations) == 0 {
		t.Fatal("failed attempts still report $ai_generation")
	}
	raw, _ := json.Marshal(h.events.named("narration_failed"))
	more, _ := json.Marshal(generations)
	for _, text := range h.texts {
		if strings.Contains(string(raw), text) || strings.Contains(string(more), text) {
			t.Fatal("events must never contain narration text")
		}
	}
}

// refusingSpeech answers with err either on voice resolution or on the paid synthesis call.
type refusingSpeech struct {
	narration.Speech
	err     error
	resolve bool
}

func (r refusingSpeech) SynthesizeNarration(ctx context.Context, in *enginev1.SynthesizeNarrationRequest, opts ...grpc.CallOption) (*enginev1.SynthesizeNarrationResponse, error) {
	if in.ResolveOnly == r.resolve {
		return nil, r.err
	}
	return r.Speech.SynthesizeNarration(ctx, in, opts...)
}

func notPromoted(t *testing.T) error {
	t.Helper()
	st, err := status.New(codes.Unavailable, "promotion_task_not_passed").WithDetails(&errdetails.ErrorInfo{Reason: "promotion_task_not_passed", Domain: engine.PromotionDomain, Metadata: map[string]string{"task": "narration"}})
	if err != nil {
		t.Fatal(err)
	}
	return st.Err()
}

// The engine's typed not-promoted refusal will not clear on retry: the first, non-final attempt
// already falls back to captions only with nothing held or charged. Ordinary UNAVAILABLE retries.
func TestConnectBoundedElevenLabsSynthesisIntegrationNotPromotedSkipsRetries(t *testing.T) {
	for _, c := range []struct {
		name    string
		err     func(*testing.T) error
		resolve bool
		retried bool
	}{
		{"refused synthesis", notPromoted, false, false},
		{"refused voice resolution", notPromoted, true, false},
		{"transient unavailable still retries", func(*testing.T) error { return status.Error(codes.Unavailable, "provider_outage") }, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newAssetHarness(t, 30*24*time.Hour)
			opts := h.opts
			opts.Speech = refusingSpeech{Speech: h.opts.Speech, err: c.err(t), resolve: c.resolve}
			service := narration.New(opts)
			if _, err := service.Queue(); err != nil {
				t.Fatal(err)
			}
			h.service = service
			h.enqueue(lesson)
			err := service.SynthesizeLesson(h.ctx, owner, lesson, false)
			want := "failed"
			if c.retried {
				want = "pending"
				if err == nil {
					t.Fatal("a transient outage must return an error so River retries")
				}
			} else if err != nil {
				t.Fatal("a not-promoted refusal must not be retried", err)
			}
			for _, slide := range []string{"sky-1", "sky-2", "sky-3"} {
				if got := h.status(lesson, slide); got != want {
					t.Fatalf("%s: status=%q want %q", slide, got, want)
				}
			}
			if h.spent() != 0 || h.openReservations() != 0 {
				t.Fatal("refusal must not charge or hold credits", h.spent(), h.openReservations())
			}
		})
	}
}

// Cancelling the caller stops synthesis without charge and leaves the slides pending for a retry.
func TestConnectBoundedElevenLabsSynthesisIntegrationCancellationChargesNothing(t *testing.T) {
	h := newAssetHarness(t, 30*24*time.Hour)
	h.enqueue(lesson)
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	if err := h.service.SynthesizeLesson(ctx, owner, lesson, false); err == nil {
		t.Fatal("a cancelled run must report an error so River can retry")
	}
	if h.spent() != 0 || h.openReservations() != 0 {
		t.Fatal("cancellation must not charge or hold credits", h.spent(), h.openReservations())
	}
	if h.status(lesson, "sky-1") != "pending" {
		t.Fatal("slides stay pending after cancellation", h.status(lesson, "sky-1"))
	}
}
