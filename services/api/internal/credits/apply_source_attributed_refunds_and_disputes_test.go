package credits

import (
	"errors"
	"testing"
)

func TestMoneyToCreditsIsCumulativeAndNeverExceedsSource(t *testing.T) {
	// 300 one-minor refunds of a 100-credit/300-minor order: per-refund rounding
	// would reverse 0; cumulative arithmetic reverses exactly the source.
	var reversed int64
	for total := int64(1); total <= 300; total++ {
		target := MoneyToCredits(100, 300, total)
		if target < reversed {
			t.Fatalf("cumulative credits went backwards at %d", total)
		}
		reversed = target
		if reversed > 100 {
			t.Fatalf("over-reversed %d", reversed)
		}
	}
	if reversed != 100 {
		t.Fatalf("full refund must reverse source credits, got %d", reversed)
	}
	if MoneyToCredits(400, 500, 250) != 200 || MoneyToCredits(400, 500, 0) != 0 || MoneyToCredits(400, 0, 5) != 0 {
		t.Fatal("boundary arithmetic")
	}
}

func TestSourceDeductionAndDebtSplit(t *testing.T) {
	for _, c := range []struct{ rev, unspent, ded, debt int64 }{
		{400, 400, 400, 0}, {200, 260, 200, 0}, {200, 60, 60, 140}, {200, 0, 0, 200}, {200, -5, 0, 200}, {0, 10, 0, 0},
	} {
		if d, b := CalculateSourceDeductionAndDebt(c.rev, c.unspent); d != c.ded || b != c.debt {
			t.Fatalf("%+v got %d %d", c, d, b)
		}
	}
}

func TestRefundCapRejectsOverRefundAndNonPositive(t *testing.T) {
	if !CheckRefundCap(500, 250, 250) || CheckRefundCap(500, 250, 251) || CheckRefundCap(500, 0, 0) || CheckRefundCap(500, 0, -1) {
		t.Fatal("cap")
	}
}

func TestReversalItemsFailClosedAndNeverDefault(t *testing.T) {
	ok := ProviderRefund{ID: "re_1", AmountMinor: 100, Currency: "usd", Status: "succeeded"}
	for name, c := range map[string]struct {
		r []ProviderRefund
		d []ProviderDispute
	}{
		"missing id":        {r: []ProviderRefund{{AmountMinor: 100, Currency: "usd", Status: "succeeded"}}},
		"missing amount":    {r: []ProviderRefund{{ID: "re_2", Currency: "usd", Status: "succeeded"}}},
		"currency mismatch": {r: []ProviderRefund{{ID: "re_3", AmountMinor: 5, Currency: "eur", Status: "succeeded"}}},
		"unknown status":    {r: []ProviderRefund{{ID: "re_4", AmountMinor: 5, Currency: "usd", Status: "mystery"}}},
		"duplicate id":      {r: []ProviderRefund{ok, ok}},
		"dispute no amount": {d: []ProviderDispute{{ID: "dp_1", Currency: "usd", Status: "lost"}}},
		"dispute status":    {d: []ProviderDispute{{ID: "dp_2", AmountMinor: 5, Currency: "usd", Status: "???"}}},
	} {
		if _, err := reversalItems(c.r, c.d, "USD"); !errors.Is(err, ErrReversalEvidence) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	items, err := reversalItems([]ProviderRefund{ok, {ID: "re_p", AmountMinor: 9, Currency: "usd", Status: "pending"}, {ID: "re_f", AmountMinor: 9, Currency: "usd", Status: "failed"}},
		[]ProviderDispute{{ID: "dp_w", AmountMinor: 9, Currency: "usd", Status: "won"}, {ID: "dp_l", AmountMinor: 7, Currency: "usd", Status: "lost"}}, "usd")
	if err != nil || len(items) != 2 || items[0].id != "re_1" || items[1].id != "dp_l" || items[1].kind != "chargeback" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
