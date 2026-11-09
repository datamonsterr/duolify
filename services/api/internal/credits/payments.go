package credits

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
)

// PaymentProvider is the server-side Checkout boundary. E7-06 supplies signed
// fixtures only; no network adapter, credentials or live sales are enabled.
type PaymentProvider interface {
	CreateSession(context.Context, PaymentRequest) (PaymentSession, error)
	RetrieveSession(context.Context, string) (PaymentSession, error)
}

// PaymentRequest contains immutable configured terms and content-free bindings.
// OrderID is also the provider idempotency key. Implementations must preserve it
// across timeouts/restarts and return the original Session on every retry.
type PaymentRequest struct {
	OrderID, OwnerReference, SKU, PriceID, Currency string
	Mode                                            string
	BillingPeriod                                   string
	AmountMinor                                     int
}

// PaymentSession is canonical provider state, including expanded price/line-item
// information. Event snapshots and browser callbacks never populate this value.
type PaymentSession struct {
	ID, OrderID, OwnerReference, SKU, PriceID, Currency string
	Mode, Status, PaymentStatus, PaymentID              string
	SubscriptionID, CustomerID                          string
	AmountMinor                                         int
	LiveMode                                            bool
}

// PaymentOptions must be configured before serving requests or starting workers.
type PaymentOptions struct {
	Mode          string
	WebhookSecret string
	LiveEnabled   bool
	Provider      PaymentProvider
}

// ConfigurePaymentEnvironment validates the deployment gate without ever
// constructing a provider or consuming real Stripe API credentials.
func (s *Service) ConfigurePaymentEnvironment(getenv func(string) string) error {
	live := false
	if value := getenv("STRIPE_LIVE_ENABLED"); value != "" {
		var err error
		live, err = strconv.ParseBool(value)
		if err != nil {
			return errPaymentConfig
		}
	}
	return s.ConfigurePayments(PaymentOptions{Mode: getenv("STRIPE_MODE"), WebhookSecret: getenv("STRIPE_WEBHOOK_SECRET"), LiveEnabled: live})
}

var errPaymentConfig = errors.New("stripe unavailable: only an explicitly injected test provider is permitted; live mode requires E7-05")
var errStripeEvidence = errors.New("invalid Stripe evidence")
var paymentIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

// ConfigurePayments fails closed even if the future live switch is requested.
func (s *Service) ConfigurePayments(options PaymentOptions) error {
	if options.LiveEnabled || (options.Mode != "" && options.Mode != "disabled" && options.Mode != "test") {
		return errPaymentConfig
	}
	if options.Mode == "test" && (options.Provider == nil || options.WebhookSecret == "") {
		return errPaymentConfig
	}
	s.payments = options
	return nil
}

// verifyStripeSignature implements Stripe's documented v1 manual verification.
// Neither JSON parsing nor durable storage may run before this check.
func verifyStripeSignature(raw []byte, header, secret string, now time.Time) error {
	if secret == "" {
		return errStripeEvidence
	}
	var stamp string
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			if stamp != "" {
				return errStripeEvidence
			}
			stamp = value
		case "v1":
			signatures = append(signatures, value)
		}
	}
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || seconds < 0 {
		return errStripeEvidence
	}
	delta := now.Sub(time.Unix(seconds, 0))
	if delta > 5*time.Minute || delta < -5*time.Minute {
		return errStripeEvidence
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stamp + "."))
	_, _ = mac.Write(raw)
	expected := mac.Sum(nil)
	for _, signature := range signatures {
		decoded, err := hex.DecodeString(signature)
		if err == nil && hmac.Equal(expected, decoded) {
			return nil
		}
	}
	return errStripeEvidence
}

func (s *Service) preparePayment(ctx context.Context, owner string, order CheckoutOrder, cfg pricing.Config) error {
	if s.payments.Mode != "test" || (order.BillingPeriod != "one_time" && order.BillingPeriod != "monthly" && order.BillingPeriod != "yearly") {
		return nil
	}
	if order.BillingPeriod == "monthly" || order.BillingPeriod == "yearly" {
		if _, ok := s.payments.Provider.(SubscriptionProvider); !ok {
			return errPaymentConfig
		}
	}
	price := cfg.ProviderMappings("development")[order.SKU]
	if price == "" {
		return errPaymentConfig
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	if err = tx.QueryRow(ctx, "SELECT id::text FROM credit_checkout_orders WHERE id=$1 AND learner_id=$2 FOR UPDATE", order.ID, owner).Scan(&id); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_payment_sessions WHERE order_id=$1)", id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return tx.Commit(ctx)
	}
	request := PaymentRequest{OrderID: id, OwnerReference: accounts.LearnerReference(owner), SKU: order.SKU, PriceID: price, AmountMinor: order.AmountMinor, Currency: order.Currency, Mode: paymentMode(order.BillingPeriod), BillingPeriod: order.BillingPeriod}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	session, err := s.payments.Provider.CreateSession(callCtx, request)
	if err != nil {
		return err
	}
	if !sessionMatches(session, request) || !paymentIdentifier.MatchString(session.ID) {
		return errStripeEvidence
	}
	if _, err = tx.Exec(ctx, "INSERT INTO credit_payment_sessions(order_id,environment,session_id,price_id) VALUES($1,'test',$2,$3)", id, session.ID, price); err != nil {
		return err
	}
	if err = purchaseOutbox(ctx, tx, owner, id+":checkout_started", "checkout_started", order, ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func sessionMatches(session PaymentSession, request PaymentRequest) bool {
	mode := request.Mode
	if mode == "" {
		mode = "payment"
	}
	return !session.LiveMode && session.Mode == mode && session.OrderID == request.OrderID && session.OwnerReference == request.OwnerReference && session.SKU == request.SKU && session.PriceID == request.PriceID && session.AmountMinor == request.AmountMinor && strings.EqualFold(session.Currency, request.Currency)
}

func (s *Service) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if s.payments.Mode != "test" || s.payments.Provider == nil || s.pool() == nil {
		respondJSON(w, 503, map[string]string{"error": "payment receipt unavailable"})
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil || verifyStripeSignature(raw, r.Header.Get("Stripe-Signature"), s.payments.WebhookSecret, time.Now()) != nil {
		respondJSON(w, 400, map[string]string{"error": "invalid signature"})
		return
	}
	var event struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		LiveMode *bool  `json:"livemode"`
		Data     struct {
			Object struct {
				ID string `json:"id"`
			} `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &event) != nil || !paymentIdentifier.MatchString(event.ID) || event.LiveMode == nil || *event.LiveMode {
		respondJSON(w, 400, map[string]string{"error": "invalid event"})
		return
	}
	switch event.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded", "checkout.session.async_payment_failed", "checkout.session.expired", "invoice.paid", "invoice.payment_succeeded", "invoice.updated", "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
	default:
		respondJSON(w, 200, map[string]bool{"received": true})
		return
	}
	if !paymentIdentifier.MatchString(event.Data.Object.ID) {
		respondJSON(w, 400, map[string]string{"error": "invalid session"})
		return
	}
	// Allowlist projection: arbitrary customer data and metadata never persist.
	redacted, _ := json.Marshal(map[string]any{"id": event.ID, "type": event.Type, "livemode": false, "data": map[string]any{"object": map[string]string{"id": event.Data.Object.ID}}})
	tx, err := s.pool().Begin(r.Context())
	if err != nil {
		respondJSON(w, 503, map[string]string{"error": "receipt unavailable"})
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	command, err := tx.Exec(r.Context(), "INSERT INTO credit_payment_inbox(event_id,environment,event_type,session_id) VALUES($1,'test',$2,$3) ON CONFLICT DO NOTHING", event.ID, event.Type, event.Data.Object.ID)
	if err == nil && command.RowsAffected() == 1 {
		_, err = tx.Exec(r.Context(), "INSERT INTO credit_payment_payloads(event_id,payload) VALUES($1,$2)", event.ID, redacted)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		respondJSON(w, 503, map[string]string{"error": "receipt unavailable"})
		return
	}
	respondJSON(w, 200, map[string]bool{"received": true})
}

// ProcessPayments retries durable inbox work. Transient provider/store failures
// leave no outcome, so the next worker or restarted process can try again.
func (s *Service) ProcessPayments(ctx context.Context) error {
	if s.payments.Mode != "test" || s.pool() == nil {
		return nil
	}
	// A provider can deliver before CreateSession's binding transaction commits.
	// Unmatched signed receipts wait durably, without starving bound work.
	rows, err := s.pool().Query(ctx, `SELECT i.event_id FROM credit_payment_inbox i WHERE (EXISTS(SELECT 1 FROM credit_payment_sessions p WHERE p.session_id=i.session_id AND p.environment=i.environment) OR i.event_type LIKE 'invoice.%' OR i.event_type LIKE 'customer.subscription.%') AND NOT EXISTS(SELECT 1 FROM credit_payment_results r WHERE r.event_id=i.event_id) ORDER BY received_at,event_id LIMIT 100`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range ids {
		if err = s.processPayment(ctx, id); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Service) processPayment(ctx context.Context, eventID string) error {
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Same lock order as reservations and settlement; caches never serialize money.
	if err = financialLock(ctx, tx); err != nil {
		return err
	}
	var processed bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_payment_results WHERE event_id=$1)", eventID).Scan(&processed); err != nil || processed {
		return err
	}
	var kind, sessionID string
	if err = tx.QueryRow(ctx, "SELECT event_type,session_id FROM credit_payment_inbox WHERE event_id=$1", eventID).Scan(&kind, &sessionID); err != nil {
		return err
	}
	if strings.HasPrefix(kind, "invoice.") || strings.HasPrefix(kind, "customer.subscription.") {
		return s.processSubscriptionEvent(ctx, tx, eventID, kind, sessionID)
	}
	var order CheckoutOrder
	var owner *string
	var ownerRef, price string
	err = tx.QueryRow(ctx, `SELECT o.id::text,o.learner_id::text,o.owner_reference,o.sku,o.pricing_version,o.amount_minor,o.currency,o.credits,o.platform,p.price_id,o.billing_period FROM credit_payment_sessions p JOIN credit_checkout_orders o ON o.id=p.order_id WHERE p.session_id=$1 AND p.environment='test' FOR UPDATE OF o`, sessionID).Scan(&order.ID, &owner, &ownerRef, &order.SKU, &order.PricingVersion, &order.AmountMinor, &order.Currency, &order.Credits, &order.Platform, &price, &order.BillingPeriod)
	if errors.Is(err, pgx.ErrNoRows) {
		return paymentResult(ctx, tx, eventID, "", "rejected", "", 0)
	}
	if err != nil {
		return err
	}
	if owner == nil {
		return paymentResult(ctx, tx, eventID, order.ID, "deleted", "", 0)
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	canonical, err := s.payments.Provider.RetrieveSession(callCtx, sessionID)
	if err != nil {
		return err
	}
	expected := PaymentRequest{OrderID: order.ID, OwnerReference: ownerRef, SKU: order.SKU, PriceID: price, AmountMinor: order.AmountMinor, Currency: order.Currency, Mode: paymentMode(order.BillingPeriod), BillingPeriod: order.BillingPeriod}
	if order.BillingPeriod == "monthly" || order.BillingPeriod == "yearly" {
		if canonical.ID != sessionID || !sessionMatches(canonical, expected) || !paymentIdentifier.MatchString(canonical.SubscriptionID) || !paymentIdentifier.MatchString(canonical.CustomerID) {
			return paymentResult(ctx, tx, eventID, order.ID, "rejected", "", 0)
		}
		if err = bindSubscription(ctx, tx, canonical.SubscriptionID, canonical.CustomerID, order.ID); err != nil {
			if errors.Is(err, errStripeEvidence) {
				return paymentResult(ctx, tx, eventID, order.ID, "rejected", "", 0)
			}
			return err
		}
		state := "pending"
		if canonical.Status == "expired" {
			state = "expired"
		} else if kind == "checkout.session.async_payment_failed" && canonical.PaymentStatus != "paid" {
			state = "failed"
		}
		return paymentResult(ctx, tx, eventID, order.ID, state, "", 0)
	}
	if canonical.ID != sessionID || !sessionMatches(canonical, expected) || (canonical.PaymentStatus == "paid" && (!paymentIdentifier.MatchString(canonical.PaymentID) || canonical.Status != "complete")) {
		if err = purchaseOutbox(ctx, tx, *owner, order.ID+":failed", "credits_purchase_failed", order, "invalid_evidence"); err != nil {
			return err
		}
		return paymentResult(ctx, tx, eventID, order.ID, "rejected", "", 0)
	}
	key := "stripe:test:" + canonical.PaymentID
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_payment_results WHERE (order_id=$1 AND state='paid') OR payment_key=$2)", order.ID, key).Scan(&processed); err != nil {
		return err
	}
	if processed {
		return paymentResult(ctx, tx, eventID, order.ID, "duplicate", "", 0)
	}
	state := "pending"
	switch canonical.PaymentStatus {
	case "paid":
		state = "paid"
	case "unpaid":
		if canonical.Status == "expired" {
			state = "expired"
		} else if kind == "checkout.session.async_payment_failed" {
			state = "failed"
		}
	}
	var ledgerID int64
	if state == "paid" {
		provenance, _ := json.Marshal(map[string]any{"bucket": "purchase", "provider": "stripe", "environment": "test", "payment_id": canonical.PaymentID, "session_id": sessionID, "sku": order.SKU, "price_id": price, "channel": order.Platform, "service_period": "one_time"})
		err = tx.QueryRow(ctx, `INSERT INTO credit_entries(learner_id,owner_reference,amount,kind,reference,pricing_version,provenance) VALUES($1,$2,$3,'purchase',$4,$5,$6) RETURNING id`, *owner, ownerRef, order.Credits, key, order.PricingVersion, provenance).Scan(&ledgerID)
		if err != nil {
			return err
		}
		if err = purchaseOutbox(ctx, tx, *owner, key+":purchased", "credits_purchased", order, ""); err != nil {
			return err
		}
	} else {
		key = ""
		if state == "failed" || state == "expired" {
			if err = purchaseOutbox(ctx, tx, *owner, order.ID+":failed", "credits_purchase_failed", order, state); err != nil {
				return err
			}
		}
	}
	if err = paymentResult(ctx, tx, eventID, order.ID, state, key, ledgerID); err != nil {
		return err
	}
	// Cache errors cannot change committed money. Force the next wallet read to
	// derive authoritative state; Redis is only a disposable display projection.
	_ = s.InvalidateCache(ctx, *owner)
	return nil
}

// paymentResult commits the outcome with every ledger/outbox write. Receipt rows
// are never updated or removed, including when validation rejects a payment.
func paymentResult(ctx context.Context, tx pgx.Tx, eventID, orderID, state, key string, ledgerID int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO credit_payment_results(event_id,order_id,state,payment_key,ledger_id) VALUES($1,NULLIF($2,'')::uuid,$3,NULLIF($4,''),NULLIF($5,0))`, eventID, orderID, state, key, ledgerID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func purchaseOutbox(ctx context.Context, tx pgx.Tx, owner, key, event string, order CheckoutOrder, reason string) error {
	props := map[string]any{"source": "api", "platform": order.Platform, "app_version": "development", "sku_id": order.SKU, "channel": "stripe", "currency": order.Currency, "amount_minor": order.AmountMinor, "price_table_version": order.PricingVersion}
	if event == "credits_purchased" {
		props["amount"] = order.Credits
		props["price"] = float64(order.AmountMinor) / 100
	}
	if reason != "" {
		props["reason"] = reason
	}
	return insertOutbox(ctx, tx, owner, key, event, props)
}

// PrunePaymentPayloads deletes only short-lived redacted diagnostic envelopes.
func (s *Service) PrunePaymentPayloads(ctx context.Context) error {
	if s.pool() == nil {
		return nil
	}
	_, err := s.pool().Exec(ctx, "DELETE FROM credit_payment_payloads WHERE expires_at<=now()")
	return err
}
