//go:build integration

package narration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
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
