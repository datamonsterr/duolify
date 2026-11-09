//go:build integration

package credits_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type packProvider struct {
	mu          sync.Mutex
	sessions    map[string]credits.PaymentSession
	unavailable bool
}

func (p *packProvider) CreateSession(_ context.Context, r credits.PaymentRequest) (credits.PaymentSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := "cs_test_" + r.OrderID
	if existing, ok := p.sessions[id]; ok {
		return existing, nil
	}
	s := credits.PaymentSession{ID: id, OrderID: r.OrderID, OwnerReference: r.OwnerReference, SKU: r.SKU, PriceID: r.PriceID, AmountMinor: r.AmountMinor, Currency: r.Currency, Mode: "payment", Status: "open", PaymentStatus: "unpaid", PaymentID: "pi_" + r.OrderID}
	p.sessions[id] = s
	return s, nil
}
func (p *packProvider) RetrieveSession(_ context.Context, id string) (credits.PaymentSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.unavailable {
		return credits.PaymentSession{}, fmt.Errorf("fixture outage")
	}
	s, ok := p.sessions[id]
	if !ok {
		return s, fmt.Errorf("missing fixture")
	}
	return s, nil
}
func signedPack(raw []byte, at time.Time) string {
	timestamp := fmt.Sprint(at.Unix())
	mac := hmac.New(sha256.New, []byte("test-only-webhook-secret"))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(raw)
	return "t=" + timestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

type packEvents struct {
	mu    sync.Mutex
	names []string
}

func (e *packEvents) Capture(id, name string, props map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := uuid.Parse(id); err != nil {
		return err
	}
	if props["source"] != "api" || props["app_version"] == nil || props["platform"] == nil {
		return fmt.Errorf("missing common properties")
	}
	e.names = append(e.names, name)
	return nil
}
func (*packEvents) Close() error { return nil }

func TestFulfillStripeCreditPacksExactlyOnce(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("pack"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	learner, err := db.Queries.CreateLearner(ctx, "Pack learner")
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.UUID(learner.ID.Bytes).String()
	if _, err = db.Pool.Exec(ctx, "INSERT INTO learner_profiles(learner_id,analytics_opt_out) VALUES($1,false)", owner); err != nil {
		t.Fatal(err)
	}
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	fake := &packProvider{sessions: map[string]credits.PaymentSession{}}
	events := new(packEvents)
	service := func() *credits.Service {
		s := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, events)
		if e := s.ConfigurePayments(credits.PaymentOptions{Mode: "test", WebhookSecret: "test-only-webhook-secret", Provider: fake}); e != nil {
			t.Fatal(e)
		}
		return s
	}
	svc := service()
	cfg := catalogue.Config()
	pack := cfg.Packs[0]
	create := func() credits.CheckoutOrder {
		o, e := svc.CreateCheckout(ctx, owner, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: pack.SKU, PricingVersion: cfg.Version, Platform: "web", ReturnTo: "/credits"})
		if e != nil {
			t.Fatal(e)
		}
		if o.Illustrative || o.State != "pending" {
			t.Fatalf("not a test checkout: %+v", o)
		}
		return o
	}
	order := create()
	sessionID := "cs_test_" + order.ID
	notify := func(id, kind string) {
		raw := []byte(fmt.Sprintf(`{"id":%q,"type":%q,"livemode":false,"data":{"object":{"id":%q,"customer_email":"private@example.test","metadata":{"draft":"private learner text"}}}}`, id, kind, sessionID))
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/payments/stripe/webhook", strings.NewReader(string(raw)))
		req.Header.Set("Stripe-Signature", signedPack(raw, time.Now()))
		w := httptest.NewRecorder()
		svc.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("receipt %d %s", w.Code, w.Body.String())
		}
	}
	count := func(query string, want int) {
		var n int
		if e := db.Pool.QueryRow(ctx, query).Scan(&n); e != nil || n != want {
			t.Fatalf("%s: got %d want %d err %v", query, n, want, e)
		}
	}
	notify("evt_delayed", "checkout.session.completed")
	if err = svc.ProcessPayments(ctx); err != nil {
		t.Fatal(err)
	}
	count("SELECT count(*) FROM credit_entries WHERE kind='purchase'", 0)
	fake.mu.Lock()
	s := fake.sessions[sessionID]
	s.PaymentStatus = "paid"
	s.Status = "complete"
	fake.sessions[sessionID] = s
	fake.unavailable = true
	fake.mu.Unlock()
	notify("evt_paid", "checkout.session.async_payment_succeeded")
	if err = svc.ProcessPayments(ctx); err == nil {
		t.Fatal("outage not retriable")
	}
	count("SELECT count(*) FROM credit_payment_inbox", 2)
	fake.mu.Lock()
	fake.unavailable = false
	fake.mu.Unlock()
	// A failure after ledger insertion must roll back the entire financial
	// transaction, while the separately committed signed receipt remains durable.
	if _, err = db.Pool.Exec(ctx, `CREATE FUNCTION fixture_reject_purchase_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event='credits_purchased' THEN RAISE EXCEPTION 'fixture outbox outage'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER fixture_reject_purchase_outbox BEFORE INSERT ON credit_outbox FOR EACH ROW EXECUTE FUNCTION fixture_reject_purchase_outbox()`); err != nil {
		t.Fatal(err)
	}
	if err = svc.ProcessPayments(ctx); err == nil {
		t.Fatal("outbox outage did not abort fulfillment")
	}
	count("SELECT count(*) FROM credit_entries WHERE kind='purchase'", 0)
	count("SELECT count(*) FROM credit_payment_results WHERE state='paid'", 0)
	count("SELECT count(*) FROM credit_outbox WHERE event='credits_purchased'", 0)
	count("SELECT count(*) FROM credit_payment_inbox", 2)
	if _, err = db.Pool.Exec(ctx, "DROP TRIGGER fixture_reject_purchase_outbox ON credit_outbox; DROP FUNCTION fixture_reject_purchase_outbox()"); err != nil {
		t.Fatal(err)
	}
	// Process restart and concurrent workers: inbox work has no memory dependency.
	svc = service()
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if e := svc.ProcessPayments(ctx); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	notify("evt_paid", "checkout.session.async_payment_succeeded")
	notify("evt_other", "checkout.session.completed")
	notify("evt_late", "checkout.session.expired")
	if err = svc.ProcessPayments(ctx); err != nil {
		t.Fatal(err)
	}
	count("SELECT count(*) FROM credit_entries WHERE kind='purchase'", 1)
	count("SELECT count(*) FROM credit_payment_results WHERE state='paid'", 1)
	count("SELECT count(*) FROM credit_outbox WHERE event='credits_purchased'", 1)
	count("SELECT count(*) FROM credit_outbox WHERE event='checkout_started'", 1)
	got, err := svc.GetCheckout(ctx, owner, order.ID)
	if err != nil || got.State != "paid" {
		t.Fatalf("not verified: %+v %v", got, err)
	}
	wallet, err := svc.GetWallet(ctx, owner)
	if err != nil || wallet.Purchased != int64(pack.Credits) {
		t.Fatalf("wrong wallet: %+v %v", wallet, err)
	}
	var payload string
	if err = db.Pool.QueryRow(ctx, "SELECT payload::text FROM credit_payment_payloads LIMIT 1").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "private") {
		t.Fatal("private payload retained")
	}
	if err = svc.DispatchOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if len(events.names) != 2 {
		t.Fatalf("delivery: %v", events.names)
	}
	if err = svc.DispatchOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if len(events.names) != 2 {
		t.Fatal("outbox duplicated")
	}
	// Canonical mismatches cannot mint, regardless of authenticated event claims.
	for _, field := range []string{"amount", "currency", "owner", "order", "sku", "price", "mode", "environment", "payment"} {
		o := create()
		sessionID = "cs_test_" + o.ID
		fake.mu.Lock()
		s = fake.sessions[sessionID]
		s.PaymentStatus = "paid"
		s.Status = "complete"
		switch field {
		case "amount":
			s.AmountMinor++
		case "currency":
			s.Currency = "EUR"
		case "owner":
			s.OwnerReference = "other"
		case "order":
			s.OrderID = uuid.NewString()
		case "sku":
			s.SKU = "other"
		case "price":
			s.PriceID = "other"
		case "mode":
			s.Mode = "subscription"
		case "environment":
			s.LiveMode = true
		case "payment":
			s.PaymentID = ""
		}
		fake.sessions[sessionID] = s
		fake.mu.Unlock()
		notify("evt_bad_"+field, "checkout.session.completed")
		if err = svc.ProcessPayments(ctx); err != nil {
			t.Fatal(err)
		}
	}
	count("SELECT count(*) FROM credit_entries WHERE kind='purchase'", 1)
	// Same business payment reported for a separate order still grants once.
	o := create()
	sessionID = "cs_test_" + o.ID
	fake.mu.Lock()
	s = fake.sessions[sessionID]
	s.PaymentStatus = "paid"
	s.Status = "complete"
	s.PaymentID = "pi_" + order.ID
	fake.sessions[sessionID] = s
	fake.mu.Unlock()
	notify("evt_same_payment", "checkout.session.completed")
	if err = svc.ProcessPayments(ctx); err != nil {
		t.Fatal(err)
	}
	count("SELECT count(*) FROM credit_entries WHERE kind='purchase'", 1)
	for _, state := range []string{"failed", "expired"} {
		o = create()
		sessionID = "cs_test_" + o.ID
		kind := "checkout.session.async_payment_failed"
		if state == "expired" {
			fake.mu.Lock()
			s = fake.sessions[sessionID]
			s.Status = "expired"
			fake.sessions[sessionID] = s
			fake.mu.Unlock()
			kind = "checkout.session.expired"
		}
		notify("evt_"+state, kind)
		if err = svc.ProcessPayments(ctx); err != nil {
			t.Fatal(err)
		}
		got, err = svc.GetCheckout(ctx, owner, o.ID)
		if err != nil || got.State != state {
			t.Fatalf("state %s: %+v %v", state, got, err)
		}
	}
	// Opt-out at dispatch suppresses already queued financial events.
	if _, err = db.Pool.Exec(ctx, "UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1", owner); err != nil {
		t.Fatal(err)
	}
	if err = svc.DispatchOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if len(events.names) != 2 {
		t.Fatal("opt-out leaked")
	}
	// Invalid raw-body signatures and old/future windows never enter the durable inbox.
	raw := []byte(`{"id":"evt_forged","type":"checkout.session.completed","livemode":false,"data":{"object":{"id":"unknown"}}}`)
	for _, signature := range []string{"", signedPack(raw, time.Now().Add(-6*time.Minute)), signedPack(raw, time.Now().Add(6*time.Minute)), signedPack(append(raw, ' '), time.Now())} {
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/payments/stripe/webhook", strings.NewReader(string(raw)))
		req.Header.Set("Stripe-Signature", signature)
		w := httptest.NewRecorder()
		svc.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("invalid signature accepted %d", w.Code)
		}
	}
	count("SELECT count(*) FROM credit_payment_inbox WHERE event_id='evt_forged'", 0)
	for _, table := range []string{"credit_payment_sessions", "credit_payment_inbox", "credit_payment_results"} {
		column := map[string]string{"credit_payment_sessions": "price_id", "credit_payment_inbox": "event_type", "credit_payment_results": "state"}[table]
		if _, err = db.Pool.Exec(ctx, "UPDATE "+table+" SET "+column+"="+column); err == nil {
			t.Fatalf("mutable payment history: %s", table)
		}
		for _, verb := range []string{"DELETE FROM ", "TRUNCATE "} {
			if _, err = db.Pool.Exec(ctx, verb+table); err == nil {
				t.Fatalf("mutable history: %s%s", verb, table)
			}
		}
	}
	// A webhook may beat the transaction that binds its provider Session. Keep
	// unmatched signed receipts pending instead of permanently rejecting them.
	earlyInput := credits.CheckoutInput{RequestID: uuid.NewString(), SKU: pack.SKU, PricingVersion: cfg.Version, Platform: "web", ReturnTo: "/credits"}
	earlyOrder, e := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, nil).CreateCheckout(ctx, owner, earlyInput)
	if e != nil {
		t.Fatal(e)
	}
	sessionID = "cs_test_" + earlyOrder.ID
	notify("evt_early_binding", "checkout.session.completed")
	if err = svc.ProcessPayments(ctx); err != nil {
		t.Fatal(err)
	}
	count("SELECT count(*) FROM credit_payment_results WHERE event_id='evt_early_binding'", 0)
	if _, err = svc.CreateCheckout(ctx, owner, earlyInput); err != nil {
		t.Fatal(err)
	}
	if err = svc.ProcessPayments(ctx); err != nil {
		t.Fatal(err)
	}
	count("SELECT count(*) FROM credit_payment_results WHERE event_id='evt_early_binding' AND state='pending'", 1)
	// Payload retention is separate from immutable receipt and business dedupe history.
	if _, err = db.Pool.Exec(ctx, "UPDATE credit_payment_payloads SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if err = svc.PrunePaymentPayloads(ctx); err != nil {
		t.Fatal(err)
	}
	count("SELECT count(*) FROM credit_payment_payloads", 0)
	count("SELECT count(*) FROM credit_payment_results WHERE state='paid'", 1)
	// Restore deletion replay severs only identity; durable keys survive.
	if _, err = db.Pool.Exec(ctx, "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1)", accounts.LearnerReference(owner)); err != nil {
		t.Fatal(err)
	}
	if err = accounts.ApplyDeletionTombstones(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.GetCheckout(ctx, owner, order.ID); err == nil {
		t.Fatal("deleted owner retained access")
	}
	sessionID = "cs_test_" + order.ID
	notify("evt_after_delete", "checkout.session.completed")
	if err = svc.ProcessPayments(ctx); err != nil {
		t.Fatal(err)
	}
	count("SELECT count(*) FROM credit_entries WHERE kind='purchase'", 1)
	var provenance map[string]any
	var amount int
	if err = db.Pool.QueryRow(ctx, "SELECT amount,provenance FROM credit_entries WHERE kind='purchase'").Scan(&amount, &provenance); err != nil {
		t.Fatal(err)
	}
	if amount != pack.Credits || provenance["payment_id"] != "pi_"+order.ID {
		t.Fatalf("lost provenance %d %s", amount, mustPackJSON(provenance))
	}
}
func mustPackJSON(value any) string { raw, _ := json.Marshal(value); return string(raw) }
