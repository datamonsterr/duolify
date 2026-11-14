package creditstest

import (
	"context"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
)

// FinancialObjects enumerates canonical fixture state, including lost notifications.
func (s *Stripe) FinancialObjects(_ context.Context, after string, limit int) ([]credits.FinancialObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var objects []credits.FinancialObject
	for id := range s.sessions {
		objects = append(objects, credits.FinancialObject{Kind: "session", ID: id})
	}
	for id := range s.invoices {
		objects = append(objects, credits.FinancialObject{Kind: "invoice", ID: id})
	}
	for id := range s.subscriptions {
		objects = append(objects, credits.FinancialObject{Kind: "subscription", ID: id})
	}
	return credits.FinancialObjectPage(objects, after, limit), nil
}
