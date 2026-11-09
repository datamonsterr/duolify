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
	"sync"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
)

// WebhookSecret is authored fake data, never a real endpoint secret.
const WebhookSecret = "fixture-only-not-a-real-stripe-secret"

// Stripe keeps canonical fixture state independently of signed event snapshots.
type Stripe struct {
	mu       sync.Mutex
	sessions map[string]credits.PaymentSession
}

// NewStripe creates an isolated, credential-free sandbox double.
func NewStripe() *Stripe { return &Stripe{sessions: map[string]credits.PaymentSession{}} }

// CreateSession preserves the order idempotency key across service retries.
func (s *Stripe) CreateSession(_ context.Context, r credits.PaymentRequest) (credits.PaymentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := "cs_test_" + r.OrderID
	if existing, ok := s.sessions[id]; ok {
		return existing, nil
	}
	session := credits.PaymentSession{ID: id, OrderID: r.OrderID, OwnerReference: r.OwnerReference, SKU: r.SKU, PriceID: r.PriceID, Currency: r.Currency, AmountMinor: r.AmountMinor, Mode: "payment", Status: "open", PaymentStatus: "unpaid", PaymentID: "pi_" + r.OrderID}
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
	sum := sha256.Sum256([]byte(id + state + kind))
	raw, err := json.Marshal(map[string]any{"id": fmt.Sprintf("evt_%x", sum[:16]), "type": kind, "livemode": false, "data": map[string]any{"object": map[string]string{"id": id}}})
	if err != nil {
		return nil, "", err
	}
	timestamp := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(WebhookSecret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(raw)
	return raw, "t=" + timestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil)), nil
}
