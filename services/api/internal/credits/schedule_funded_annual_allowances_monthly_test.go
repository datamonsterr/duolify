package credits

import (
	"testing"
	"time"
)

func TestAnnualFundingEvidence(t *testing.T) {
	start := time.Date(2024, 2, 29, 12, 30, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 12, 30, 0, 0, time.UTC)
	r := PaymentRequest{OrderID: "order", OwnerReference: "owner", SKU: "configured-year", PriceID: "price_year", Currency: "USD", AmountMinor: 9600, Mode: "subscription"}
	sub := ProviderSubscription{ID: "sub_fixture", OrderID: r.OrderID, OwnerReference: r.OwnerReference, CustomerID: "cus_fixture", PriceID: r.PriceID, Status: "active", Interval: "year", IntervalCount: 1, Quantity: 1, PeriodStart: start, PeriodEnd: end, BillingAnchor: start}
	invoice := SubscriptionInvoice{ID: "in_fixture", SubscriptionID: sub.ID, OrderID: r.OrderID, OwnerReference: r.OwnerReference, CustomerID: sub.CustomerID, SKU: r.SKU, PriceID: r.PriceID, Currency: r.Currency, Status: "paid", BillingReason: "subscription_create", AmountMinor: r.AmountMinor, AmountPaid: r.AmountMinor, Quantity: 1, LineCount: 1, PeriodStart: start, PeriodEnd: end, StripePaid: true}
	if !annualFundingMatches(invoice, sub, r, start) {
		t.Fatal("valid leap-year funding rejected")
	}
	for _, invalid := range []string{"month", "count", "short", "long", "unpaid", "proration", "trial", "future", "owner", "refunded", "disputed", "anchor", "sub_period", "discount"} {
		t.Run(invalid, func(t *testing.T) {
			i, s := invoice, sub
			switch invalid {
			case "month":
				s.Interval = "month"
			case "count":
				s.IntervalCount = 2
			case "short":
				i.PeriodEnd = end.Add(-time.Hour)
			case "long":
				i.PeriodEnd = end.Add(time.Hour)
			case "unpaid":
				i.AmountPaid--
			case "proration":
				i.Proration = true
			case "trial":
				s.Trial = true
			case "future":
				i.PeriodStart = start.Add(time.Hour)
			case "owner":
				i.OwnerReference = "other"
			case "refunded":
				i.Refunded = true
			case "disputed":
				i.Disputed = true
			case "anchor":
				s.BillingAnchor = start.AddDate(0, 1, 0)
			case "sub_period":
				s.PeriodEnd = end.Add(-time.Hour)
			case "discount":
				i.Adjusted = true
			}
			if annualFundingMatches(i, s, r, start) {
				t.Fatal("invalid funding accepted")
			}
		})
	}
	sub.CancelAtPeriodEnd = true
	if !annualFundingMatches(invoice, sub, r, start) {
		t.Fatal("cancelled renewal invalidated funded term")
	}
}

func TestAnnualServiceMonthBoundaries(t *testing.T) {
	anchor := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	for _, tc := range []struct {
		index int
		want  string
	}{{0, "2024-01-31T23:59:59Z"}, {1, "2024-02-29T23:59:59Z"}, {2, "2024-03-31T23:59:59Z"}, {3, "2024-04-30T23:59:59Z"}, {12, "2025-01-31T23:59:59Z"}} {
		if got := annualServiceMonth(anchor, tc.index).Format(time.RFC3339); got != tc.want {
			t.Fatalf("month %d: %s want %s", tc.index, got, tc.want)
		}
	}
}
