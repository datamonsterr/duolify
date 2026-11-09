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
	mu            sync.Mutex
	sessions      map[string]credits.PaymentSession
	invoices      map[string]credits.SubscriptionInvoice
	subscriptions map[string]credits.ProviderSubscription
}

// NewStripe creates an isolated, credential-free sandbox double.
func NewStripe() *Stripe {
	return &Stripe{sessions: map[string]credits.PaymentSession{}, invoices: map[string]credits.SubscriptionInvoice{}, subscriptions: map[string]credits.ProviderSubscription{}}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, "", errors.New("unknown fixture session")
	}
	switch state {
	case "cancel_scheduled", "refunded", "disputed":
		// Canonical subscription/invoice changes are applied below.
	case "paid":
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
	s.sessions[id] = session
	objectID := id
	if session.Mode == "subscription" {
		if strings.HasPrefix(kind, "invoice.") {
			objectID = "in_" + session.OrderID
			i := s.invoices[objectID]
			if state == "refunded" {
				i.Refunded = true
			}
			if state == "disputed" {
				i.Disputed = true
			}
			if state == "paid" {
				i.Status = "paid"
				i.AmountPaid = i.AmountMinor
				i.StripePaid = true
			}
			s.invoices[objectID] = i
			sub := s.subscriptions[session.SubscriptionID]
			if state == "paid" {
				sub.Status = "active"
			}
			s.subscriptions[sub.ID] = sub
		} else if strings.HasPrefix(kind, "customer.subscription.") {
			objectID = session.SubscriptionID
			if state == "cancel_scheduled" {
				sub := s.subscriptions[objectID]
				sub.CancelAtPeriodEnd = true
				s.subscriptions[objectID] = sub
			}
		}
	}
	sum := sha256.Sum256([]byte(id + state + kind))
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
