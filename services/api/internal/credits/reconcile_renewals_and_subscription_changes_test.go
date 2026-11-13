package credits

import (
	"context"
	"testing"
	"time"
)

func TestLifecycleUsesFundingNotDunningPeriod(t *testing.T) {
	end := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		provider, want          string
		cancel, revoked, active bool
	}{
		{"active", "active", false, false, true}, {"active", "cancel-scheduled", true, false, true},
		{"past_due", "past-due", false, false, true}, {"unpaid", "expired", false, false, false},
		{"canceled", "cancel-scheduled", false, false, true}, {"trialing", "expired", false, false, false},
		{"incomplete", "expired", false, false, false}, {"incomplete_expired", "expired", false, false, false},
		{"paused", "expired", false, false, false}, {"future_status", "expired", false, false, false},
		{"active", "refunded", false, true, false},
	} {
		p := FundedPlan{PaidThrough: end, Active: true, Revoked: tc.revoked}
		applyLifecycle(&p, tc.provider, tc.cancel, "refunded", end.Add(-time.Second))
		if p.Status != tc.want || p.Active != tc.active {
			t.Fatalf("%+v => %+v", tc, p)
		}
	}
	p := FundedPlan{PaidThrough: end, Active: false}
	applyLifecycle(&p, "active", false, "", end)
	if p.Status != "expired" || p.Active {
		t.Fatal("extended funding", p)
	}
}

type scheduleRecorder struct{ got SubscriptionTransition }

func (r *scheduleRecorder) ScheduleTransition(_ context.Context, v SubscriptionTransition) error {
	r.got = v
	return nil
}
func TestStripeTransitionUsesNextRenewalWithoutProration(t *testing.T) {
	recorder := new(scheduleRecorder)
	var transitioner SubscriptionTransitioner = StripeTransitioner{Scheduler: recorder}
	end := time.Now().UTC().Add(time.Hour)
	sub := ProviderSubscription{ID: "sub_fixture", Status: "active", PeriodEnd: end}
	got, err := transitioner.Transition(t.Context(), sub, SubscriptionTransition{Key: "intent", PriceID: "price_configured"})
	if err != nil || !got.EffectiveAt.Equal(end) || got.ProrationBehavior != "none" || got.PhaseProrationBehavior != "none" || recorder.got.PriceID != "price_configured" {
		t.Fatalf("%+v %v", got, err)
	}
	sub.LiveMode = true
	if _, err = transitioner.Transition(t.Context(), sub, SubscriptionTransition{}); err == nil {
		t.Fatal("live mode enabled")
	}
}
