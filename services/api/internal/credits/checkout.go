package credits

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errCheckoutInput = errors.New("checkout unavailable; refresh prices, choose a permitted offer and review terms")

// CheckoutInput contains identifiers only. Financial terms are always resolved server-side.
type CheckoutInput struct {
	RequestID      string `json:"request_id"`
	SKU            string `json:"sku"`
	PricingVersion string `json:"pricing_version"`
	Platform       string `json:"platform"`
	ReturnTo       string `json:"return_to"`
	QuoteID        string `json:"quote_id,omitempty"`
}

// CheckoutOrder is a server-bound illustrative order, never payment evidence.
type CheckoutOrder struct {
	ID             string `json:"id"`
	State          string `json:"state"`
	Illustrative   bool   `json:"illustrative"`
	SKU            string `json:"sku"`
	PricingVersion string `json:"pricing_version"`
	AmountMinor    int    `json:"amount_minor"`
	Currency       string `json:"currency"`
	Credits        int    `json:"credits"`
	BillingPeriod  string `json:"billing_period"`
	ReturnTo       string `json:"return_to"`
	QuoteID        string `json:"quote_id"`
}

func safeCheckoutReturn(target string) bool {
	switch target {
	case "/credits", "/learn", "/notes", "/courses":
		return true
	}
	return false
}

func checkoutTerms(cfg pricing.Config, in CheckoutInput) (CheckoutOrder, error) {
	order := CheckoutOrder{State: "pending", Illustrative: true, SKU: in.SKU, PricingVersion: cfg.Version, ReturnTo: in.ReturnTo, QuoteID: in.QuoteID}
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return order, errCheckoutInput
	}
	if !safeCheckoutReturn(in.ReturnTo) || in.PricingVersion != cfg.Version || (in.Platform != "web" && in.Platform != "desktop") {
		return order, errCheckoutInput
	}
	if in.QuoteID != "" {
		if _, err := uuid.Parse(in.QuoteID); err != nil {
			return order, errCheckoutInput
		}
	}
	for _, pack := range cfg.Packs {
		if pack.SKU == in.SKU {
			order.AmountMinor = pack.AmountMinor
			order.Currency = pack.Currency
			order.Credits = pack.Credits
			order.BillingPeriod = "one_time"
			return order, nil
		}
	}
	for _, plan := range cfg.Plans {
		for period, offer := range map[string]pricing.Offer{"monthly": plan.Monthly, "yearly": plan.Yearly} {
			if offer.SKU == in.SKU {
				order.AmountMinor = offer.AmountMinor
				order.Currency = offer.Currency
				order.Credits = plan.MonthlyCredits
				order.BillingPeriod = period
				return order, nil
			}
		}
	}
	return order, errCheckoutInput
}

// CreateCheckout pins illustrative terms. It never calls a payment service or changes the ledger.
func (s *Service) CreateCheckout(ctx context.Context, owner string, in CheckoutInput) (CheckoutOrder, error) {
	if s.pool() == nil {
		return CheckoutOrder{}, errors.New("checkout store unavailable")
	}
	catalogue, err := pricing.Load(ctx, db.New(s.pool()))
	if err != nil {
		return CheckoutOrder{}, err
	}
	order, err := checkoutTerms(catalogue.Config(), in)
	if err != nil {
		return order, err
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return order, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if in.QuoteID != "" {
		var owned bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pricing_quotes WHERE id=$1 AND learner_id=$2)", in.QuoteID, owner).Scan(&owned)
		if err != nil {
			return order, err
		}
		if !owned {
			return order, errCheckoutInput
		}
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO credit_checkout_orders(request_id,learner_id,owner_reference,pricing_version,sku,amount_minor,currency,credits,billing_period,platform,return_to,quote_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,'')::uuid) ON CONFLICT(owner_reference,request_id) DO NOTHING RETURNING id::text`, in.RequestID, owner, accounts.LearnerReference(owner), order.PricingVersion, order.SKU, order.AmountMinor, order.Currency, order.Credits, order.BillingPeriod, in.Platform, in.ReturnTo, in.QuoteID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// A retry must describe the exact same intent, including channel and action.
		err = tx.QueryRow(ctx, `SELECT id::text FROM credit_checkout_orders WHERE learner_id=$1 AND request_id=$2 AND sku=$3 AND pricing_version=$4 AND platform=$5 AND return_to=$6 AND COALESCE(quote_id::text,'')=$7`, owner, in.RequestID, in.SKU, in.PricingVersion, in.Platform, in.ReturnTo, in.QuoteID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return order, errCheckoutInput
		}
	}
	if err != nil {
		return order, err
	}
	if err = tx.Commit(ctx); err != nil {
		return order, err
	}
	order.ID = id
	return order, nil
}

// GetCheckout reads authenticated server state. Illustrations cannot become verified,
// including when callbacks claim success or unrelated purchases change the balance.
func (s *Service) GetCheckout(ctx context.Context, owner, id string) (CheckoutOrder, error) {
	order := CheckoutOrder{State: "pending", Illustrative: true}
	if _, err := uuid.Parse(id); err != nil {
		return order, pgx.ErrNoRows
	}
	if s.pool() == nil {
		return order, errors.New("checkout store unavailable")
	}
	err := s.pool().QueryRow(ctx, `SELECT id::text,sku,pricing_version,amount_minor,currency,credits,billing_period,return_to,COALESCE(quote_id::text,'') FROM credit_checkout_orders WHERE id=$1 AND learner_id=$2`, id, owner).Scan(&order.ID, &order.SKU, &order.PricingVersion, &order.AmountMinor, &order.Currency, &order.Credits, &order.BillingPeriod, &order.ReturnTo, &order.QuoteID)
	return order, err
}

func (s *Service) handleCheckout(w http.ResponseWriter, r *http.Request) {
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		respondJSON(w, 401, map[string]string{"error": "sign in required"})
		return
	}
	var order CheckoutOrder
	var err error
	if r.Method == http.MethodPost {
		var in CheckoutInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		var extra any
		if decoder.Decode(&in) != nil || decoder.Decode(&extra) != io.EOF {
			respondJSON(w, 400, map[string]string{"error": "invalid checkout input"})
			return
		}
		order, err = s.CreateCheckout(r.Context(), owner, in)
	} else {
		order, err = s.GetCheckout(r.Context(), owner, r.PathValue("id"))
	}
	if err != nil {
		code := 503
		if errors.Is(err, errCheckoutInput) {
			code = 409
		}
		if errors.Is(err, pgx.ErrNoRows) {
			code = 404
		}
		respondJSON(w, code, map[string]string{"error": "Checkout unavailable. Refresh prices and review your selection."})
		return
	}
	respondJSON(w, 200, order)
}
