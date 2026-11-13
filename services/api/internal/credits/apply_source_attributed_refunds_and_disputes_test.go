package credits

import (
	"testing"
)

func TestSourceAttributionRefundArithmetic(t *testing.T) {
	tests := []struct {
		name               string
		sourceAmount       int64
		priorSpends        int64
		orderAmountMinor   int
		orderCredits       int
		refundAmountMinor  int
		expectedCreditsRev int64
		expectedDeducted   int64
		expectedDebt       int64
	}{
		{
			name:               "full refund of untouched pack",
			sourceAmount:       400,
			priorSpends:        0,
			orderAmountMinor:   500,
			orderCredits:       400,
			refundAmountMinor:  500,
			expectedCreditsRev: 400,
			expectedDeducted:   400,
			expectedDebt:       0,
		},
		{
			name:               "partial refund of partly spent pack within unspent",
			sourceAmount:       400,
			priorSpends:        140, // 260 unspent
			orderAmountMinor:   500,
			orderCredits:       400,
			refundAmountMinor:  250, // 50% -> 200 credits
			expectedCreditsRev: 200,
			expectedDeducted:   200,
			expectedDebt:       0,
		},
		{
			name:               "refund exceeds unspent source - records debt",
			sourceAmount:       400,
			priorSpends:        300, // 100 unspent
			orderAmountMinor:   500,
			orderCredits:       400,
			refundAmountMinor:  250, // 50% -> 200 credits
			expectedCreditsRev: 200,
			expectedDeducted:   100, // only 100 unspent available
			expectedDebt:       100, // 100 deficit
		},
		{
			name:               "chargeback on depleted source - full debt",
			sourceAmount:       400,
			priorSpends:        400, // 0 unspent
			orderAmountMinor:   500,
			orderCredits:       400,
			refundAmountMinor:  500, // 400 credits
			expectedCreditsRev: 400,
			expectedDeducted:   0,
			expectedDebt:       400, // entire amount becomes debt
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			unspent := tc.sourceAmount - tc.priorSpends
			if unspent < 0 {
				unspent = 0
			}
			creditsRev := CalculateRefundCredits(tc.orderCredits, tc.orderAmountMinor, tc.refundAmountMinor)
			if creditsRev != tc.expectedCreditsRev {
				t.Fatalf("expected creditsRev %d, got %d", tc.expectedCreditsRev, creditsRev)
			}
			deducted, debt := CalculateSourceDeductionAndDebt(creditsRev, unspent)
			if deducted != tc.expectedDeducted {
				t.Fatalf("expected deducted %d, got %d", tc.expectedDeducted, deducted)
			}
			if debt != tc.expectedDebt {
				t.Fatalf("expected debt %d, got %d", tc.expectedDebt, debt)
			}
		})
	}
}

func TestCumulativeRefundCapCheck(t *testing.T) {
	orderAmountMinor := 1500

	// Already refunded $10.00 (1000 minor)
	alreadyRefundedMinor := 1000

	// Another refund of $5.00 (500 minor) -> exact total $15.00 -> OK
	if !CheckRefundCap(orderAmountMinor, alreadyRefundedMinor, 500) {
		t.Fatal("expected exact cap to be valid")
	}

	// Another refund of $5.01 (501 minor) -> exceeds $15.00 -> Rejected
	if CheckRefundCap(orderAmountMinor, alreadyRefundedMinor, 501) {
		t.Fatal("expected exceeding cap to be invalid")
	}
}
