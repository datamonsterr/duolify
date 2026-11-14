package credits

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// FinancialObject is a canonical provider object reference, never webhook terms.
// Key is a stable pagination key comprising kind and provider object id.
type FinancialObject struct{ Kind, ID string }

// Key orders the canonical inventory independently from notification delivery.
func (o FinancialObject) Key() string { return o.Kind + ":" + o.ID }

// FinancialProvider lists the complete canonical TEST inventory, including
// invoices whose notifications never arrived. Pagination is stable and ordered.
type FinancialProvider interface {
	SubscriptionProvider
	FinancialObjects(context.Context, string, int) ([]FinancialObject, error)
}

// SignedFinancialFixtures is an independently supplied, read-only TEST provider.
// It is never constructed from restored ledger rows and cannot create a charge.
type SignedFinancialFixtures struct {
	Mode          string                 `json:"mode"`
	Sessions      []PaymentSession       `json:"sessions"`
	Invoices      []SubscriptionInvoice  `json:"invoices"`
	Subscriptions []ProviderSubscription `json:"subscriptions"`
}

// LoadSignedFinancialFixtures verifies a hex HMAC-SHA256 over the exact file bytes
// before decoding. The owner supplies the canonical fixture file and separate key.
// It intentionally has no network adapter or live mode.
func LoadSignedFinancialFixtures(raw []byte, signature, secret string) (*SignedFinancialFixtures, error) {
	if len(raw) > 10<<20 || secret == "" {
		return nil, errStripeEvidence
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	sig, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errStripeEvidence
	}
	var f SignedFinancialFixtures
	if json.Unmarshal(raw, &f) != nil || f.Mode != "test" {
		return nil, errStripeEvidence
	}
	seen := map[string]bool{}
	add := func(kind, id string, live bool) bool {
		k := kind + ":" + id
		if live || !paymentIdentifier.MatchString(id) || seen[k] {
			return false
		}
		seen[k] = true
		return true
	}
	for _, v := range f.Sessions {
		if !add("session", v.ID, v.LiveMode) {
			return nil, errStripeEvidence
		}
	}
	for _, v := range f.Invoices {
		if !add("invoice", v.ID, v.LiveMode) {
			return nil, errStripeEvidence
		}
	}
	for _, v := range f.Subscriptions {
		if !add("subscription", v.ID, v.LiveMode) {
			return nil, errStripeEvidence
		}
	}
	return &f, nil
}

// CreateSession is unavailable: restore verification never initiates purchases.
func (*SignedFinancialFixtures) CreateSession(context.Context, PaymentRequest) (PaymentSession, error) {
	return PaymentSession{}, errPaymentConfig
}

// RetrieveSession reads canonical fixture evidence.
func (f *SignedFinancialFixtures) RetrieveSession(_ context.Context, id string) (PaymentSession, error) {
	for _, v := range f.Sessions {
		if v.ID == id {
			return v, nil
		}
	}
	return PaymentSession{}, errStripeEvidence
}

// RetrieveInvoice reads canonical fixture evidence.
func (f *SignedFinancialFixtures) RetrieveInvoice(_ context.Context, id string) (SubscriptionInvoice, error) {
	for _, v := range f.Invoices {
		if v.ID == id {
			return v, nil
		}
	}
	return SubscriptionInvoice{}, errStripeEvidence
}

// RetrieveSubscription reads canonical fixture evidence.
func (f *SignedFinancialFixtures) RetrieveSubscription(_ context.Context, id string) (ProviderSubscription, error) {
	for _, v := range f.Subscriptions {
		if v.ID == id {
			return v, nil
		}
	}
	return ProviderSubscription{}, errStripeEvidence
}

// FinancialObjects provides stable bounded inventory pages.
func (f *SignedFinancialFixtures) FinancialObjects(_ context.Context, after string, limit int) ([]FinancialObject, error) {
	var objects []FinancialObject
	for _, v := range f.Sessions {
		objects = append(objects, FinancialObject{"session", v.ID})
	}
	for _, v := range f.Invoices {
		objects = append(objects, FinancialObject{"invoice", v.ID})
	}
	for _, v := range f.Subscriptions {
		objects = append(objects, FinancialObject{"subscription", v.ID})
	}
	return FinancialObjectPage(objects, after, limit), nil
}

// FinancialObjectPage bounds and sorts fixture inventory pages without mutating input.
func FinancialObjectPage(objects []FinancialObject, after string, limit int) []FinancialObject {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	page := append([]FinancialObject(nil), objects...)
	sort.Slice(page, func(i, j int) bool { return page[i].Key() < page[j].Key() })
	i := sort.Search(len(page), func(i int) bool { return page[i].Key() > after })
	return page[i:min(i+limit, len(page))]
}
