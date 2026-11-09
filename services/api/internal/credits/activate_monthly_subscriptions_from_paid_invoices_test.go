package credits

import (
	"testing"
	"time"
)

func TestMonthlyFundingEvidenceBoundaries(t *testing.T) {
	start := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	request := PaymentRequest{OrderID: "order", OwnerReference: "owner", SKU: "configured-monthly", PriceID: "configured-price", Currency: "USD", AmountMinor: 999, Mode: "subscription"}
	sub := ProviderSubscription{ID: "sub_fixture", OrderID: "order", OwnerReference: "owner", CustomerID: "cus_fixture", PriceID: "configured-price", Status: "active", Interval: "month", IntervalCount: 1, Quantity: 1, PeriodStart: start, PeriodEnd: end, BillingAnchor: start}
	invoice := SubscriptionInvoice{ID: "in_fixture", SubscriptionID: sub.ID, OrderID: "order", OwnerReference: "owner", CustomerID: sub.CustomerID, SKU: request.SKU, PriceID: request.PriceID, Currency: "usd", AmountMinor: 999, AmountPaid: 999, Status: "paid", BillingReason: "subscription_create", PeriodStart: start, PeriodEnd: end, Quantity: 1, LineCount: 1, StripePaid: true}
	if !monthlyFundingMatches(invoice, sub, request, start.Add(time.Second)) {
		t.Fatal("valid clamped monthly period rejected")
	}
	for _, field := range []string{"unpaid", "zero", "underpaid", "currency", "owner", "order", "sku", "price", "subscription", "customer", "live", "trial", "yearly", "quantity", "lines", "proration", "reason", "off_stripe", "adjustment", "future", "period", "sub_period"} {
		t.Run(field, func(t *testing.T) {
			i, s := invoice, sub
			switch field {
			case "unpaid":
				i.Status = "open"
			case "zero":
				i.AmountPaid = 0
			case "underpaid":
				i.AmountPaid--
			case "currency":
				i.Currency = "EUR"
			case "owner":
				i.OwnerReference = "other"
			case "order":
				i.OrderID = "other"
			case "sku":
				i.SKU = "other"
			case "price":
				i.PriceID = "other"
			case "subscription":
				i.SubscriptionID = "sub_other"
			case "customer":
				i.CustomerID = "cus_other"
			case "live":
				i.LiveMode = true
			case "trial":
				s.Trial = true
			case "yearly":
				s.Interval = "year"
			case "quantity":
				i.Quantity = 2
			case "lines":
				i.LineCount = 2
			case "proration":
				i.Proration = true
			case "reason":
				i.BillingReason = "subscription_update"
			case "off_stripe":
				i.StripePaid = false
			case "adjustment":
				i.Adjusted = true
			case "future":
				i.PeriodStart = start.AddDate(0, 1, 0)
			case "period":
				i.PeriodEnd = end.Add(time.Hour)
			case "sub_period":
				s.PeriodEnd = end.Add(-time.Hour)
			}
			if monthlyFundingMatches(i, s, request, start.Add(time.Second)) {
				t.Fatal("invalid terms accepted")
			}
		})
	}
	i := invoice
	i.BillingReason = "subscription_cycle"
	if !monthlyFundingMatches(i, sub, request, start.Add(time.Second)) {
		t.Fatal("funded cycle rejected")
	}
	// The original Jan 31 anchor resumes on Mar 31 after February's clamp.
	i.PeriodStart = end
	i.PeriodEnd = time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	sub.PeriodStart = i.PeriodStart
	sub.PeriodEnd = i.PeriodEnd
	sub.BillingAnchor = start
	if !monthlyFundingMatches(i, sub, request, end.Add(time.Second)) {
		t.Fatal("original month-end billing anchor lost after February")
	}
}
