// Package creditstest supplies a sandbox-style provider only to test binaries.
// No production command imports this package or exposes its notification controls.
package creditstest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
)

// WebhookSecret is authored fake data, never a real endpoint secret.
const WebhookSecret = "fixture-only-not-a-real-stripe-secret"

// Stripe keeps canonical fixture state independently of signed event snapshots.
type Stripe struct {
	mu              sync.Mutex
	transitions     map[string]credits.SubscriptionTransition
	currentInvoices map[string]string
	sessions        map[string]credits.PaymentSession
	invoices        map[string]credits.SubscriptionInvoice
	subscriptions   map[string]credits.ProviderSubscription
}

// NewStripe creates an isolated, credential-free sandbox double.
func NewStripe() *Stripe {
	return &Stripe{currentInvoices: map[string]string{}, transitions: map[string]credits.SubscriptionTransition{}, sessions: map[string]credits.PaymentSession{}, invoices: map[string]credits.SubscriptionInvoice{}, subscriptions: map[string]credits.ProviderSubscription{}}
}

// CreateSession preserves the order idempotency key across service retries.
func (s *Stripe) CreateSession(_ context.Context, r credits.PaymentRequest) (credits.PaymentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := "cs_test_" + r.OrderID
	if existing, ok := s.sessions[id]; ok {
		return existing, nil
	}
	session := credits.PaymentSession{ID: id, OrderID: r.OrderID, OwnerReference: r.OwnerReference, SKU: r.SKU, PriceID: r.PriceID, Currency: r.Currency, AmountMinor: r.AmountMinor, Mode: "payment", Status: "open", PaymentStatus: "unpaid", PaymentID: "pi_" + r.OrderID}
	if r.Mode == "subscription" {
		session.Mode = "subscription"
		session.SubscriptionID = "sub_" + r.OrderID
		session.CustomerID = "cus_" + r.OwnerReference
		start := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
		// Fixture starts are safely before the 28th, avoiding billing-anchor ambiguity.
		start = time.Date(start.Year(), start.Month(), 1, start.Hour(), start.Minute(), start.Second(), 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		interval := "month"
		if r.BillingPeriod == "yearly" {
			interval = "year"
			end = start.AddDate(1, 0, 0)
		}
		s.subscriptions[session.SubscriptionID] = credits.ProviderSubscription{ID: session.SubscriptionID, OrderID: r.OrderID, OwnerReference: r.OwnerReference, CustomerID: session.CustomerID, PriceID: r.PriceID, Status: "incomplete", Interval: interval, IntervalCount: 1, Quantity: 1, PeriodStart: start, PeriodEnd: end, BillingAnchor: start}
		s.currentInvoices[session.SubscriptionID] = "in_" + r.OrderID
		s.invoices["in_"+r.OrderID] = credits.SubscriptionInvoice{ID: "in_" + r.OrderID, SubscriptionID: session.SubscriptionID, OrderID: r.OrderID, OwnerReference: r.OwnerReference, CustomerID: session.CustomerID, SKU: r.SKU, PriceID: r.PriceID, Currency: r.Currency, AmountMinor: r.AmountMinor, Status: "open", BillingReason: "subscription_create", PeriodStart: start, PeriodEnd: end, Quantity: 1, LineCount: 1}
	}
	s.sessions[id] = session
	return session, nil
}

// RetrieveSession returns canonical state rather than an event's stale snapshot.
func (s *Stripe) RetrieveSession(_ context.Context, id string) (credits.PaymentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return session, errors.New("unknown fixture session")
	}
	return session, nil
}

// Notification updates canonical state independently of the event snapshot and
// signs the exact payload as Stripe does. Stable event IDs exercise replay too.
func (s *Stripe) Notification(id, state, kind string) ([]byte, string, error) {
	return s.NotificationWithDetails(id, state, kind, 0, "")
}

// NotificationWithDetails updates canonical state with explicit refund amount or reversal ID.
func (s *Stripe) NotificationWithDetails(id, state, kind string, amountMinor int, reversalID string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, "", errors.New("unknown fixture session")
	}
	switch state {
	case "cancel_scheduled", "resumed", "stale", "refunded", "partially_refunded", "disputed":
		// Canonical subscription/invoice changes are applied below.
	case "paid", "previous_paid", "renewed":
		session.PaymentStatus = "paid"
		session.Status = "complete"
	case "unpaid":
		session.PaymentStatus = "unpaid"
	case "expired":
		session.Status = "expired"
	case "failed":
		session.PaymentStatus = "unpaid"
	default:
		return nil, "", errors.New("unknown fixture state")
	}
	reversals := 0
	if session.Mode == "payment" {
		if state == "refunded" || state == "partially_refunded" || state == "disputed" {
			session.Refunds, session.Disputes = reverseCharge(state, session.AmountMinor, session.Currency, amountMinor, reversalID, session.OrderID, session.Refunds, session.Disputes)
			reversals = len(session.Refunds) + len(session.Disputes)
		}
	}
	s.sessions[id] = session
	objectID := id
	if session.Mode == "subscription" {
		if strings.HasPrefix(kind, "invoice.") {
			objectID = s.currentInvoices[session.SubscriptionID]
			i := s.invoices[objectID]
			sub := s.subscriptions[session.SubscriptionID]
			if state == "previous_paid" && i.Status != "paid" {
				sub.PeriodEnd = sub.PeriodStart
				sub.PeriodStart = sub.PeriodStart.AddDate(0, -1, 0)
				sub.BillingAnchor = sub.PeriodStart
				i.PeriodStart = sub.PeriodStart
				i.PeriodEnd = sub.PeriodEnd
			}
			if state == "renewed" {
				objectID = "in_renewed_" + session.OrderID
				if existing, ok := s.invoices[objectID]; ok {
					i = existing
				} else {
					sub.PeriodStart = sub.PeriodEnd
					sub.PeriodEnd = sub.PeriodStart.AddDate(0, 1, 0)
					i.ID = objectID
					i.PeriodStart = sub.PeriodStart
					i.PeriodEnd = sub.PeriodEnd
					i.BillingReason = "subscription_cycle"
				}
			}
			if state == "refunded" || state == "partially_refunded" || state == "disputed" {
				i.Refunds, i.Disputes = reverseCharge(state, i.AmountMinor, i.Currency, amountMinor, reversalID, session.OrderID, i.Refunds, i.Disputes)
				reversals = len(i.Refunds) + len(i.Disputes)
			}
			if state == "paid" || state == "previous_paid" || state == "renewed" {
				i.Status = "paid"
				i.AmountPaid = i.AmountMinor
				i.StripePaid = true
			}
			s.invoices[objectID] = i
			s.currentInvoices[session.SubscriptionID] = objectID
			if state == "failed" {
				sub.Status = "past_due"
				i.Status = "open"
				i.StripePaid = false
				i.AmountPaid = 0
				s.invoices[objectID] = i
			}
			if state == "paid" || state == "previous_paid" || state == "renewed" {
				sub.Status = "active"
			}
			sub.LatestInvoiceID = objectID
			s.subscriptions[sub.ID] = sub
		} else if strings.HasPrefix(kind, "customer.subscription.") {
			objectID = session.SubscriptionID
			if state == "resumed" {
				sub := s.subscriptions[objectID]
				sub.CancelAtPeriodEnd = false
				sub.Status = "active"
				s.subscriptions[objectID] = sub
			}
			if state == "cancel_scheduled" {
				sub := s.subscriptions[objectID]
				sub.CancelAtPeriodEnd = true
				s.subscriptions[objectID] = sub
			}
		}
	}
	sum := sha256.Sum256([]byte(fmt.Sprint(id, state, kind, amountMinor, reversalID, reversals)))
	raw, err := json.Marshal(map[string]any{"id": fmt.Sprintf("evt_%x", sum[:16]), "type": kind, "livemode": false, "data": map[string]any{"object": map[string]string{"id": objectID}}})
	if err != nil {
		return nil, "", err
	}
	timestamp := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(WebhookSecret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(raw)
	return raw, "t=" + timestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil)), nil
}

// RetrieveInvoice returns canonical expanded monthly line evidence.
func (s *Stripe) RetrieveInvoice(_ context.Context, id string) (credits.SubscriptionInvoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.invoices[id]
	if !ok {
		return v, errors.New("unknown fixture invoice")
	}
	return v, nil
}

// RetrieveSubscription returns canonical item-level period evidence.
func (s *Stripe) RetrieveSubscription(_ context.Context, id string) (credits.ProviderSubscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.subscriptions[id]
	if !ok {
		return v, errors.New("unknown fixture subscription")
	}
	return v, nil
}

// SetAnnualStart controls canonical fixture time without changing a process clock.
// It is used only by test binaries and never by a production payment adapter.
func (s *Stripe) SetAnnualStart(sessionID string, start time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return errors.New("unknown fixture session")
	}
	sub := s.subscriptions[session.SubscriptionID]
	if sub.Interval != "year" {
		return errors.New("not an annual fixture")
	}
	sub.PeriodStart = start
	sub.BillingAnchor = start
	// Fixture callers use first-of-month dates; leap/month-end validation lives in
	// the canonical evidence and scheduler integration/unit tests.
	sub.PeriodEnd = start.AddDate(1, 0, 0)
	s.subscriptions[sub.ID] = sub
	i := s.invoices["in_"+session.OrderID]
	i.PeriodStart = start
	i.PeriodEnd = sub.PeriodEnd
	s.invoices[i.ID] = i
	return nil
}

// ScheduleTransition simulates Stripe's two phases, preserving the current item.
// The idempotency key is durable in the fixture for provider/DB retry tests.
func (s *Stripe) ScheduleTransition(_ context.Context, v credits.SubscriptionTransition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[v.SubscriptionID]
	if !ok || v.ProrationBehavior != "none" || v.PhaseProrationBehavior != "none" || !v.EffectiveAt.Equal(sub.PeriodEnd) {
		return errors.New("invalid fixture schedule")
	}
	if old, ok := s.transitions[v.Key]; ok {
		if old != v {
			return errors.New("conflicting fixture schedule")
		}
		return nil
	}
	switch v.Kind {
	case "cancel":
		sub.CancelAtPeriodEnd = true
	case "resume":
		sub.CancelAtPeriodEnd = false
	case "plan":
		if v.PriceID == "" {
			return errors.New("missing configured price")
		}
	default:
		return errors.New("unknown transition")
	}
	s.transitions[v.Key] = v
	s.subscriptions[sub.ID] = sub
	return nil
}

// reverseCharge appends one canonical provider refund or dispute object. Each
// object has its own id; an existing id is never duplicated (provider replay).
// Fixture-side defaults only choose what a real provider would have decided:
// a full refund takes the remaining refundable amount, a partial one half of it.
func reverseCharge(state string, total int, currency string, amountMinor int, id, order string, refunds []credits.ProviderRefund, disputes []credits.ProviderDispute) ([]credits.ProviderRefund, []credits.ProviderDispute) {
	used := 0
	for _, r := range refunds {
		used += r.AmountMinor
	}
	for _, d := range disputes {
		used += d.AmountMinor
	}
	remaining := total - used
	if amountMinor <= 0 {
		amountMinor = remaining
		if state == "partially_refunded" {
			amountMinor = remaining / 2
		}
	}
	if state == "disputed" {
		if id == "" {
			id = fmt.Sprintf("dp_%s_%d", order, len(disputes)+1)
		}
		for _, d := range disputes {
			if d.ID == id {
				return refunds, disputes
			}
		}
		return refunds, append(disputes, credits.ProviderDispute{ID: id, AmountMinor: amountMinor, Currency: currency, Status: "needs_response"})
	}
	if id == "" {
		id = fmt.Sprintf("re_%s_%d", order, len(refunds)+1)
	}
	for _, r := range refunds {
		if r.ID == id {
			return refunds, disputes
		}
	}
	return append(refunds, credits.ProviderRefund{ID: id, AmountMinor: amountMinor, Currency: currency, Status: "succeeded"}), disputes
}
