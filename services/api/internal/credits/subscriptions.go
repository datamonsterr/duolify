package credits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SubscriptionProvider extends the existing test payment boundary. Implementations
// retrieve canonical objects; webhook snapshots never supply financial terms.
type SubscriptionProvider interface {
	PaymentProvider
	RetrieveInvoice(context.Context, string) (SubscriptionInvoice, error)
	RetrieveSubscription(context.Context, string) (ProviderSubscription, error)
}

// SubscriptionInvoice projects one expanded recurring invoice line. PeriodStart
// and PeriodEnd MUST come from lines.data[].period, never invoice.period_*.
// StripePaid excludes marked-paid/out-of-band invoices; Adjusted covers discounts,
// customer credits, credit notes, taxes and other unsupported adjustments.
type SubscriptionInvoice struct {
	ID, SubscriptionID, OrderID, OwnerReference, CustomerID, SKU, PriceID, Currency string
	Status, BillingReason                                                           string
	AmountMinor, AmountPaid, Quantity, LineCount                                    int
	PeriodStart, PeriodEnd                                                          time.Time
	LiveMode, Proration, StripePaid, Adjusted                                       bool
}

// ProviderSubscription projects the sole subscription item's current period and
// recurring price. Current Stripe APIs expose periods on items, not the root.
type ProviderSubscription struct {
	ID, OrderID, OwnerReference, CustomerID, PriceID, Status, Interval string
	IntervalCount, Quantity                                            int
	PeriodStart, PeriodEnd                                             time.Time
	BillingAnchor                                                      time.Time
	LiveMode, Trial                                                    bool
}

// FundedPlan is a database-derived entitlement snapshot, independent of balance.
type FundedPlan struct {
	PlanID               string    `json:"plan_id"`
	Name                 string    `json:"name"`
	PricingVersion       string    `json:"pricing_version"`
	EntitlementVersion   string    `json:"entitlement_version"`
	MonthlyCredits       int       `json:"monthly_credits"`
	Benefits             []string  `json:"benefits"`
	ConcurrentGeneration int       `json:"concurrent_generation"`
	PaidThrough          time.Time `json:"paid_through"`
	Active               bool      `json:"active"`
	Channel              string    `json:"channel"`
	ManagementURL        string    `json:"management_url"`
}

func paymentMode(period string) string {
	if period == "monthly" {
		return "subscription"
	}
	return "payment"
}

func serviceBoundary(year int, month time.Month, anchor time.Time) time.Time {
	anchor = anchor.UTC()
	first := time.Date(year, month, 1, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), time.UTC)
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	day := anchor.Day()
	if day > last {
		day = last
	}
	return first.AddDate(0, 0, day-1)
}

func monthlyFundingMatches(i SubscriptionInvoice, s ProviderSubscription, r PaymentRequest, now time.Time) bool {
	return r.Mode == "subscription" && !i.LiveMode && !s.LiveMode && !s.Trial && s.Status == "active" && s.Interval == "month" && s.IntervalCount == 1 && s.Quantity == 1 &&
		i.Status == "paid" && i.StripePaid && !i.Adjusted && !i.Proration && i.Quantity == 1 && i.LineCount == 1 &&
		(i.BillingReason == "subscription_create" || i.BillingReason == "subscription_cycle") &&
		paymentIdentifier.MatchString(i.ID) && paymentIdentifier.MatchString(s.ID) && paymentIdentifier.MatchString(s.CustomerID) &&
		i.SubscriptionID == s.ID && i.CustomerID == s.CustomerID && i.OrderID == r.OrderID && s.OrderID == r.OrderID && i.OwnerReference == r.OwnerReference && s.OwnerReference == r.OwnerReference &&
		i.SKU == r.SKU && i.PriceID == r.PriceID && s.PriceID == r.PriceID && strings.EqualFold(i.Currency, r.Currency) && i.AmountMinor == r.AmountMinor && i.AmountPaid == r.AmountMinor && r.AmountMinor > 0 &&
		!s.BillingAnchor.IsZero() && !i.PeriodStart.Before(s.BillingAnchor) && !i.PeriodStart.After(now) &&
		i.PeriodStart.Equal(serviceBoundary(i.PeriodStart.UTC().Year(), i.PeriodStart.UTC().Month(), s.BillingAnchor)) &&
		i.PeriodEnd.Equal(serviceBoundary(i.PeriodStart.UTC().Year(), i.PeriodStart.UTC().Month()+1, s.BillingAnchor)) && !s.PeriodStart.After(i.PeriodStart) && !s.PeriodEnd.Before(i.PeriodEnd)
}

func bindSubscription(ctx context.Context, tx pgx.Tx, id, customer, order string) error {
	_, err := tx.Exec(ctx, `INSERT INTO credit_subscriptions(subscription_id,environment,order_id,customer_id) VALUES($1,'test',$2,$3) ON CONFLICT DO NOTHING`, id, order, customer)
	if err != nil {
		return err
	}
	var matches bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_subscriptions WHERE subscription_id=$1 AND order_id=$2 AND customer_id=$3)", id, order, customer).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return errStripeEvidence
	}
	return nil
}

func (s *Service) processSubscriptionEvent(ctx context.Context, tx pgx.Tx, event, kind, object string) error {
	provider, ok := s.payments.Provider.(SubscriptionProvider)
	if !ok {
		return paymentResult(ctx, tx, event, "", "rejected", "", 0)
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.HasPrefix(kind, "customer.subscription.") {
		// Lifecycle notifications cannot erase/extend a paid term, including reordered
		// cancellation snapshots. Later lifecycle tickets handle canonical changes.
		sub, err := provider.RetrieveSubscription(callCtx, object)
		if err != nil {
			return err
		}
		if sub.ID != object || sub.LiveMode {
			return paymentResult(ctx, tx, event, "", "rejected", "", 0)
		}
		return paymentResult(ctx, tx, event, "", "pending", "", 0)
	}
	invoice, err := provider.RetrieveInvoice(callCtx, object)
	if err != nil {
		return err
	}
	if invoice.ID != object {
		return paymentResult(ctx, tx, event, "", "rejected", "", 0)
	}
	if _, err = uuid.Parse(invoice.OrderID); err != nil {
		return paymentResult(ctx, tx, event, "", "rejected", "", 0)
	}
	var order CheckoutOrder
	var owner *string
	var ownerRef, price string
	err = tx.QueryRow(ctx, `SELECT o.id::text,o.learner_id::text,o.owner_reference,o.sku,o.pricing_version,o.amount_minor,o.currency,o.credits,o.platform,o.billing_period,p.price_id FROM credit_checkout_orders o JOIN credit_payment_sessions p ON p.order_id=o.id WHERE o.id=$1 AND p.environment='test' FOR UPDATE OF o`, invoice.OrderID).Scan(&order.ID, &owner, &ownerRef, &order.SKU, &order.PricingVersion, &order.AmountMinor, &order.Currency, &order.Credits, &order.Platform, &order.BillingPeriod, &price)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} // receipt can precede session binding
	if err != nil {
		return err
	}
	if owner == nil {
		return paymentResult(ctx, tx, event, order.ID, "deleted", "", 0)
	}
	if !paymentIdentifier.MatchString(invoice.SubscriptionID) {
		return paymentResult(ctx, tx, event, order.ID, "rejected", "", 0)
	}
	sub, err := provider.RetrieveSubscription(callCtx, invoice.SubscriptionID)
	if err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		return err
	}
	request := PaymentRequest{OrderID: order.ID, OwnerReference: ownerRef, SKU: order.SKU, PriceID: price, Currency: order.Currency, AmountMinor: order.AmountMinor, Mode: paymentMode(order.BillingPeriod)}
	if order.BillingPeriod != "monthly" || !monthlyFundingMatches(invoice, sub, request, now) {
		return paymentResult(ctx, tx, event, order.ID, "rejected", "", 0)
	}
	// Preserve original configured terms even after pricing activation/rollback.
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version=$1", order.PricingVersion).Scan(&raw); err != nil {
		return err
	}
	cfg, err := pricing.Parse(raw)
	if err != nil {
		return err
	}
	var plan *pricing.Plan
	for _, p := range cfg.Plans {
		if p.Monthly.SKU == order.SKU {
			value := p
			plan = &value
			break
		}
	}
	if plan == nil || plan.MonthlyCredits != order.Credits || cfg.ProviderMappings("development")[order.SKU] != price {
		return paymentResult(ctx, tx, event, order.ID, "rejected", "", 0)
	}
	key := fmt.Sprintf("stripe:test:%s:%d:%d:0", sub.ID, invoice.PeriodStart.Unix(), invoice.PeriodEnd.Unix())
	var duplicate bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_subscription_terms WHERE period_key=$1 OR invoice_id=$2)`, key, invoice.ID).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate {
		return paymentResult(ctx, tx, event, order.ID, "duplicate", "", 0)
	}
	// Global financialLock serializes this check with checkout and other funding.
	var overlap bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_subscription_terms t JOIN credit_subscriptions s USING(subscription_id) JOIN credit_checkout_orders o ON o.id=s.order_id WHERE o.owner_reference=$1 AND t.service_start<$3 AND t.paid_through>$2)`, ownerRef, invoice.PeriodStart, invoice.PeriodEnd).Scan(&overlap); err != nil {
		return err
	}
	if overlap {
		return paymentResult(ctx, tx, event, order.ID, "rejected", "", 0)
	}
	if err = bindSubscription(ctx, tx, sub.ID, sub.CustomerID, order.ID); err != nil {
		if errors.Is(err, errStripeEvidence) {
			return paymentResult(ctx, tx, event, order.ID, "rejected", "", 0)
		}
		return err
	}
	provenance, _ := json.Marshal(map[string]any{"bucket": "subscription", "provider": "stripe", "environment": "test", "subscription_id": sub.ID, "invoice_id": invoice.ID, "plan_id": plan.ID, "period_key": key, "service_start": invoice.PeriodStart, "paid_through": invoice.PeriodEnd, "price_id": price})
	var ledger int64
	if err = tx.QueryRow(ctx, `INSERT INTO credit_entries(learner_id,owner_reference,amount,kind,reference,pricing_version,provenance) VALUES($1,$2,$3,'subscription_grant',$4,$5,$6) RETURNING id`, *owner, ownerRef, plan.MonthlyCredits, key, order.PricingVersion, provenance).Scan(&ledger); err != nil {
		return err
	}
	benefits, _ := json.Marshal(plan.Benefits)
	if _, err = tx.Exec(ctx, `INSERT INTO credit_subscription_terms(period_key,subscription_id,invoice_id,pricing_version,plan_id,plan_name,entitlement_version,service_start,paid_through,amount_minor,currency,monthly_credits,benefits,concurrent_generation,ledger_id) VALUES($1,$2,$3,$4,$5,$6,$4,$7,$8,$9,$10,$11,$12,$13,$14)`, key, sub.ID, invoice.ID, order.PricingVersion, plan.ID, plan.Name, invoice.PeriodStart, invoice.PeriodEnd, order.AmountMinor, order.Currency, plan.MonthlyCredits, benefits, plan.ConcurrentGeneration, ledger); err != nil {
		return err
	}
	for _, name := range []string{"plan_purchased", "subscription_grant_issued", "entitlement_changed"} {
		props := map[string]any{"source": "api", "platform": order.Platform, "app_version": "development", "plan_id": plan.ID, "interval": "monthly", "channel": "stripe", "amount_minor": order.AmountMinor, "currency": order.Currency, "billing_period_start": invoice.PeriodStart.UTC().Format(time.RFC3339), "billing_period_end": invoice.PeriodEnd.UTC().Format(time.RFC3339), "price_table_version": order.PricingVersion, "amount": plan.MonthlyCredits, "period_key": key, "entitlement_version": order.PricingVersion}
		eventKey := key + ":" + name
		if name == "plan_purchased" {
			eventKey = "stripe:test:" + sub.ID + ":plan_purchased"
		}
		if err = insertOutbox(ctx, tx, *owner, eventKey, name, props); err != nil {
			return err
		}
	}
	if err = paymentResult(ctx, tx, event, order.ID, "funded", key, ledger); err != nil {
		return err
	}
	_ = s.InvalidateCache(ctx, *owner)
	return nil
}

func (s *Service) fundedPlan(ctx context.Context, owner string, at time.Time) (*FundedPlan, error) {
	if s.pool() == nil {
		return nil, nil
	}
	p := &FundedPlan{Channel: "stripe", ManagementURL: "/credits/billing"}
	var benefits []byte
	err := s.pool().QueryRow(ctx, `SELECT t.plan_id,t.plan_name,t.pricing_version,t.entitlement_version,t.monthly_credits,t.benefits,t.concurrent_generation,t.paid_through,t.service_start<=$2 AND t.paid_through>$2 FROM credit_subscription_terms t JOIN credit_subscriptions s USING(subscription_id) JOIN credit_checkout_orders o ON o.id=s.order_id WHERE o.learner_id=$1 ORDER BY t.paid_through DESC LIMIT 1`, owner, at).Scan(&p.PlanID, &p.Name, &p.PricingVersion, &p.EntitlementVersion, &p.MonthlyCredits, &benefits, &p.ConcurrentGeneration, &p.PaidThrough, &p.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(benefits, &p.Benefits); err != nil {
		return nil, err
	}
	return p, nil
}
