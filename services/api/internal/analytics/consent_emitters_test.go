package analytics

import (
	"context"
	"errors"
	"testing"
)

type recordingEvents struct{ names []string }

func (e *recordingEvents) Capture(_, name string, _ map[string]any) error {
	e.names = append(e.names, name)
	return nil
}
func (*recordingEvents) Close() error                    { return nil }
func allowConsent(context.Context, string) (bool, error) { return true, nil }

func TestConsentGateSuppressesLessonAndCreditEmitters(t *testing.T) {
	for _, event := range []string{"lesson_generated", "course_generation_failed", "lesson_generation_requested", "retry_variant_queued", "retry_variant_served", "retry_variant_answered", "retry_variant_generation_failed", "daily_credits_expired", "daily_credits_granted", "credits_reserved", "credits_settled", "credits_released", "credits_insufficient", "credits_exhausted"} {
		for _, mode := range []string{"missing", "opted_out", "error", "opted_in"} {
			t.Run(event+"/"+mode, func(t *testing.T) {
				sink := new(recordingEvents)
				calls := 0
				var resolver ConsentResolver
				if mode != "missing" {
					resolver = func(context.Context, string) (bool, error) {
						calls++
						if mode == "error" {
							return true, errors.New("unavailable")
						}
						return mode == "opted_in", nil
					}
				}
				gate := WithConsent(sink, resolver)
				for range 2 {
					if err := gate.Capture("11111111-1111-4111-8111-111111111111", event, nil); err != nil {
						t.Fatal(err)
					}
				}
				want := 0
				if mode == "opted_in" {
					want = 2
				}
				if len(sink.names) != want {
					t.Fatalf("delivered %v", sink.names)
				}
				if mode != "missing" && mode != "opted_in" && calls != 1 {
					t.Fatalf("denial not cached: calls=%d", calls)
				}
			})
		}
	}
}

func TestConsentWithdrawalIsImmediate(t *testing.T) {
	sink := new(recordingEvents)
	allowed := true
	gate := WithConsent(sink, func(context.Context, string) (bool, error) { return allowed, nil })
	_ = gate.Capture("learner", "lesson_generated", nil)
	allowed = false
	_ = gate.Capture("learner", "daily_credits_expired", nil)
	if len(sink.names) != 1 {
		t.Fatal(sink.names)
	}
}

func TestDatabaseConsentKeepsOperationalIdentitySeparate(t *testing.T) {
	resolver := DatabaseConsent(nil)
	if allowed, err := resolver(t.Context(), "00000000-0000-4000-8000-000000000011"); err != nil || !allowed {
		t.Fatal("operational identity denied", err)
	}
	if allowed, err := resolver(t.Context(), "11111111-1111-4111-8111-111111111111"); err == nil || allowed {
		t.Fatal("unavailable learner database did not fail closed")
	}
}
