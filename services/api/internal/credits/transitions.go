package credits

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SubscriptionTransition is a provider-neutral request with verified timing.
// Native adapters may implement immediate net-funded transitions in E7-12/13.
type SubscriptionTransition struct {
	Key, SubscriptionID, PriceID, Kind        string
	EffectiveAt                               time.Time
	ProrationBehavior, PhaseProrationBehavior string
}

// SubscriptionTransitioner determines and applies the provider's change timing.
type SubscriptionTransitioner interface {
	Transition(context.Context, ProviderSubscription, SubscriptionTransition) (SubscriptionTransition, error)
}

// SubscriptionScheduler preserves the current phase and schedules a future one.
// Both proration controls MUST be applied; changing an item immediately is unsafe.
type SubscriptionScheduler interface {
	ScheduleTransition(context.Context, SubscriptionTransition) error
}

// StripeTransitioner applies next-renewal, non-prorated changes in test mode only.
type StripeTransitioner struct{ Scheduler SubscriptionScheduler }

// Transition preserves current terms and schedules the configured change at renewal.
func (s StripeTransitioner) Transition(ctx context.Context, sub ProviderSubscription, v SubscriptionTransition) (SubscriptionTransition, error) {
	if s.Scheduler == nil || sub.LiveMode || sub.Trial || sub.Status != "active" || !sub.PeriodEnd.After(time.Now()) {
		return v, errCheckoutInput
	}
	v.SubscriptionID = sub.ID
	v.EffectiveAt = sub.PeriodEnd
	v.ProrationBehavior = "none"
	v.PhaseProrationBehavior = "none"
	return v, s.Scheduler.ScheduleTransition(ctx, v)
}

type transitionTerms struct {
	Order CheckoutOrder `json:"order"`
	Plan  pricing.Plan  `json:"plan"`
}

// PendingPlan describes a schedule, never a funded entitlement.
type PendingPlan struct {
	Name        string    `json:"name"`
	EffectiveAt time.Time `json:"effective_at"`
}

// ChangeSubscriptionInput contains identifiers only; all financial terms are config-owned.
type ChangeSubscriptionInput struct {
	RequestID      string `json:"request_id"`
	Kind           string `json:"kind"`
	SKU            string `json:"sku"`
	PricingVersion string `json:"pricing_version"`
}

// ChangeSubscription pins the selected configuration before invoking the adapter.
// Retrying after provider success/DB failure uses the same provider idempotency key.
func (s *Service) ChangeSubscription(ctx context.Context, owner string, in ChangeSubscriptionInput) (SubscriptionTransition, error) {
	var result SubscriptionTransition
	if s.pool() == nil || s.payments.Mode != "test" {
		return result, errCheckoutInput
	}
	provider, ok := s.payments.Provider.(SubscriptionProvider)
	if !ok {
		return result, errCheckoutInput
	}
	scheduler, ok := s.payments.Provider.(SubscriptionScheduler)
	if !ok {
		return result, errCheckoutInput
	}
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return result, errCheckoutInput
	}
	if in.Kind != "plan" && in.Kind != "cancel" && in.Kind != "resume" {
		return result, errCheckoutInput
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = financialLock(ctx, tx); err != nil {
		return result, err
	}
	var subID, period, version string
	var paidThrough time.Time
	err = tx.QueryRow(ctx, `SELECT b.subscription_id,o.billing_period,t.pricing_version,t.paid_through FROM credit_subscriptions b JOIN credit_checkout_orders o ON o.id=b.order_id JOIN credit_subscription_terms t USING(subscription_id) WHERE o.learner_id=$1 AND t.service_start<=now() AND t.paid_through>now() AND NOT EXISTS(SELECT 1 FROM credit_subscription_revocations r WHERE r.period_key=t.period_key) ORDER BY t.paid_through DESC LIMIT 1`, owner).Scan(&subID, &period, &version, &paidThrough)
	if err != nil {
		return result, errCheckoutInput
	}
	// Idempotent retries cannot change an earlier intent, or disclose another owner.
	var oldKind, oldSKU, oldVersion string
	err = tx.QueryRow(ctx, `SELECT kind,COALESCE(terms->'order'->>'sku',''),pricing_version,effective_at FROM credit_subscription_transitions WHERE request_id=$1 AND subscription_id=$2`, in.RequestID, subID).Scan(&oldKind, &oldSKU, &oldVersion, &result.EffectiveAt)
	if err == nil {
		if oldKind != in.Kind || (in.Kind == "plan" && (oldSKU != in.SKU || oldVersion != in.PricingVersion)) {
			return result, errCheckoutInput
		}
		result.Kind = in.Kind
		result.Key = in.RequestID
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sub, err := provider.RetrieveSubscription(callCtx, subID)
	if err != nil {
		return result, err
	}
	if sub.ID != subID || !sub.PeriodEnd.Equal(paidThrough) {
		return result, errCheckoutInput
	}
	var bound bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_subscriptions b JOIN credit_checkout_orders o ON o.id=b.order_id WHERE b.subscription_id=$1 AND b.customer_id=$2 AND o.id::text=$3 AND o.owner_reference=$4)`, sub.ID, sub.CustomerID, sub.OrderID, sub.OwnerReference).Scan(&bound); err != nil {
		return result, err
	}
	if !bound {
		return result, errCheckoutInput
	}
	terms := transitionTerms{}
	price := ""
	if in.Kind == "plan" {
		if sub.CancelAtPeriodEnd {
			return result, errCheckoutInput
		}
		cat, e := pricing.Load(ctx, db.New(s.pool()))
		if e != nil {
			return result, e
		}
		terms.Order, e = checkoutTerms(cat.Config(), CheckoutInput{RequestID: in.RequestID, SKU: in.SKU, PricingVersion: in.PricingVersion, Platform: "web", ReturnTo: "/credits"})
		if e != nil || terms.Order.BillingPeriod != period {
			return result, errCheckoutInput
		}
		for _, p := range cat.Config().Plans {
			if p.Monthly.SKU == in.SKU || p.Yearly.SKU == in.SKU {
				terms.Plan = p
			}
		}
		price = cat.Config().ProviderMappings("development")[in.SKU]
		version = cat.Config().Version
		var pending bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_subscription_transitions WHERE subscription_id=$1 AND kind='plan' AND effective_at=$2)`, subID, paidThrough).Scan(&pending); err != nil {
			return result, err
		}
		if pending || price == "" || price == sub.PriceID {
			return result, errCheckoutInput
		}
	}
	result, err = (StripeTransitioner{Scheduler: scheduler}).Transition(callCtx, sub, SubscriptionTransition{Key: in.RequestID, Kind: in.Kind, PriceID: price})
	if err != nil {
		return result, err
	}
	raw, _ := json.Marshal(terms)
	if _, err = tx.Exec(ctx, `INSERT INTO credit_subscription_transitions(request_id,subscription_id,kind,effective_at,pricing_version,terms,price_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, in.RequestID, subID, in.Kind, result.EffectiveAt, version, raw, price); err != nil {
		return result, err
	}
	sub, err = provider.RetrieveSubscription(callCtx, subID)
	if err != nil {
		return result, err
	}
	if err = s.recordLifecycle(ctx, tx, "", sub, ""); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Service) handleSubscriptionChange(w http.ResponseWriter, r *http.Request) {
	var in ChangeSubscriptionInput
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&in) != nil || !errors.Is(d.Decode(&extra), io.EOF) {
		respondJSON(w, 400, map[string]string{"error": "Invalid subscription change."})
		return
	}
	result, err := s.ChangeSubscription(r.Context(), accounts.LearnerID(r.Context()), in)
	if err != nil {
		respondJSON(w, 409, map[string]string{"error": "Could not change your plan. Refresh your wallet and retry."})
		return
	}
	respondJSON(w, 200, result)
}
