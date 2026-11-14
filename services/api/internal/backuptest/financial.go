package backuptest

import "github.com/datamonsterr/duolify/services/api/internal/credits/creditstest"

// FinancialProvider exposes the independent canonical TEST fixture, never rows
// reconstructed from a restored financial snapshot.
func (f *Fixture) FinancialProvider() *creditstest.Stripe { return f.paymentFixture }
