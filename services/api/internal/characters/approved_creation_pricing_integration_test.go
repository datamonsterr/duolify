//go:build integration

package characters_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/google/uuid"
)

func TestApprovedCharacterPricingLifecycle(t *testing.T) {
	f := backuptest.New(t)
	ctx := t.Context()
	owner, other := uuid.NewString(), uuid.NewString()
	t.Setenv("PRICING_OWNER_ID", owner)
	for _, id := range []string{owner, other} {
		if _, err := f.DB.Pool.Exec(ctx, `INSERT INTO learners(id,display_name) VALUES($1,'pricing fixture');`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.DB.Pool.Exec(ctx, `INSERT INTO pricing_admin_grants(account_id) VALUES($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	backuptest.ActivateLegacyPricingBaseline(t, f.DB.Pool)
	cat, err := pricing.Load(ctx, f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	wallet := credits.NewServiceWithCatalogue(f.DB.Pool, nil, cat, nil)
	s := characters.NewService(f.DB.Pool, nil, nil)
	mapper := new(createMapper)
	s.ConfigureScreening(newScreeningRPC(t))
	s.ConfigureCreation(wallet, newMappingRPC(t, mapper), "production", false)
	if s.CreateAvailability(ctx).Enabled {
		t.Fatal("creation enabled without approved active key")
	}
	var cfg map[string]any
	if err := f.DB.Pool.QueryRow(ctx, `SELECT config FROM pricing_versions WHERE version='usd-dev-character-create-unapproved'`).Scan(&cfg); err != nil {
		t.Fatal(err)
	}
	cfg["version"] = "usd-character-create-approval-test"
	effective := time.Now().UTC().Add(30 * time.Second)
	cfg["effective_from"] = effective.Format(time.RFC3339Nano)
	cfg["character_create_decision"] = "owner_approved"
	cfg["actions"].(map[string]any)["character_create"] = map[string]any{"credits": 25}
	admin := pricing.NewAdmin(f.DB.Pool, nil, owner)
	admin.SetEnvironment("development")
	admin.SetVerifier(&pricing.DevelopmentVerifier{})
	call := func(actor, path string, payload any, want int) {
		t.Helper()
		body, _ := json.Marshal(payload)
		code, out := admin.ExecuteCLI(ctx, actor, http.MethodPost, path, body)
		if code != want {
			t.Fatalf("%s: status=%d want=%d body=%s", path, code, want, out)
		}
	}
	path := "/admin/v1/pricing/drafts/" + cfg["version"].(string)
	call(other, "/admin/v1/pricing/drafts", map[string]any{"config": cfg, "reason": "ADR 0013 price"}, http.StatusCreated)
	call(other, path+"/submit", map[string]string{"reason": "submit owner decision"}, http.StatusOK)
	call(other, path+"/approve", map[string]string{"reason": "non-owner rejected"}, http.StatusForbidden)
	call(owner, path+"/approve", map[string]string{"reason": "owner approves price"}, http.StatusOK)
	if s.CreateAvailability(ctx).Enabled {
		t.Fatal("approval without activation enabled creation")
	}
	timer := time.NewTimer(time.Until(effective) + 10*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	runtime := pricing.NewRuntimeCatalogue(cat, f.DB.Queries, f.DB.Pool, nil)
	runtime.SetEnvironment("development")
	runtime.SetOwnerID(owner)
	if err := runtime.ActivateDue(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if a := s.CreateAvailability(ctx); !a.Enabled || a.Proposal {
		t.Fatalf("approved availability=%+v", a)
	}
	for _, configuredOwner := range []string{"", other} {
		t.Setenv("PRICING_OWNER_ID", configuredOwner)
		if s.CreateAvailability(ctx).Enabled {
			t.Fatal("missing/mismatched pricing owner enabled creation")
		}
		if _, err := pricing.NewQuotes(f.DB.Pool).Create(ctx, owner, pricing.QuoteInput{Action: "character_create"}); err == nil {
			t.Fatal("generic quote route bypassed pricing owner check")
		}
	}
	t.Setenv("PRICING_OWNER_ID", owner)
	job, err := s.QuoteCreate(ctx, owner, characters.CreateRequest{RequestKey: "approved-create", Prompt: "An original moon gardener"})
	if err != nil || job.Quote == nil {
		t.Fatalf("quote=%+v err=%v", job, err)
	}
	if job.Quote.ContentCeiling != 25 || job.Quote.DailyFreeEligible {
		t.Fatalf("terms=%+v", job.Quote)
	}
	if _, err := wallet.EnsureDailyGrant(ctx, owner, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := wallet.GetWallet(ctx, owner)
	if err != nil || before.DailyFree.Available < 25 {
		t.Fatalf("daily grant=%+v %v", before, err)
	}
	if _, err := s.ConfirmCreate(ctx, owner, job.ID, job.Quote.ID); err == nil {
		t.Fatal("daily-free credits funded creation")
	}
	if _, err := f.DB.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,$2,'purchase','approved-funding',$3)`, owner, job.Quote.ContentCeiling*2, job.Quote.PricingVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmCreate(ctx, owner, job.ID, job.Quote.ID); err != nil {
		t.Fatal(err)
	}
	held, err := wallet.GetWallet(ctx, owner)
	if err != nil || held.Reserved != 25 || held.DailyFree.Available != before.DailyFree.Available {
		t.Fatalf("reservation=%+v %v", held, err)
	}
	if err := s.ExecuteCreate(ctx, owner, job.ID); err != nil {
		t.Fatal(err)
	}
	result, err := s.GetCreate(ctx, owner, job.ID)
	if err != nil || result.State != "succeeded" {
		t.Fatalf("result=%+v %v", result, err)
	}
	after, err := wallet.GetWallet(ctx, owner)
	if err != nil || after.Purchased != 25 || after.Reserved != 0 || after.DailyFree.Available != before.DailyFree.Available {
		t.Fatalf("settlement=%+v %v", after, err)
	}
	var spent int64
	if err := f.DB.Pool.QueryRow(ctx, `SELECT -coalesce(sum(amount),0) FROM credit_entries WHERE learner_id=$1 AND kind='spend'`, owner).Scan(&spent); err != nil || spent != 25 {
		t.Fatalf("spent=%d %v", spent, err)
	}
	regen, err := s.QuoteCreate(ctx, owner, characters.CreateRequest{RequestKey: "approved-regenerate", Prompt: "A different original gardener", CharacterID: result.Character.ID, ExpectedVersion: 1})
	if err != nil || regen.Quote == nil || regen.Quote.ContentCeiling != 25 || regen.Quote.DailyFreeEligible {
		t.Fatalf("regen=%+v %v", regen, err)
	}
	// A new activated version without the key disables new work and confirmations.
	if _, err := f.DB.Pool.Exec(ctx, `INSERT INTO pricing_versions(version,effective_from,approved,config) SELECT 'usd-character-rollback',now(),true,jsonb_set(jsonb_set(config,'{version}','"usd-character-rollback"'),'{effective_from}',to_jsonb(now())) FROM pricing_versions WHERE version='usd-quotes-2026-11-09'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.Pool.Exec(ctx, `INSERT INTO pricing_quote_policies(version,policy) SELECT 'usd-character-rollback',policy FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09'; INSERT INTO pricing_activation_events(version) VALUES('usd-character-rollback')`); err != nil {
		t.Fatal(err)
	}
	if s.CreateAvailability(ctx).Enabled {
		t.Fatal("rollback resurrected approved key")
	}
	if _, err := s.ConfirmCreate(ctx, owner, regen.ID, regen.Quote.ID); err == nil {
		t.Fatal("rollback allowed confirmation")
	}
	original, err := pricing.NewQuotes(f.DB.Pool).Get(ctx, owner, job.Quote.ID)
	if err != nil || original.ContentCeiling != 25 || original.PricingVersion != job.Quote.PricingVersion {
		t.Fatalf("immutable quote=%+v %v", original, err)
	}
	// A claimed decision without the owner's audited review cannot activate,
	// and a forged activation marker cannot bypass either runtime entry point.
	cfg["version"] = "usd-character-unaudited"
	cfg["effective_from"] = time.Now().UTC().Format(time.RFC3339Nano)
	raw, _ := json.Marshal(cfg)
	if _, err := f.DB.Pool.Exec(ctx, `INSERT INTO pricing_versions(version,effective_from,approved,config) VALUES($1,$2,true,$3)`, cfg["version"], cfg["effective_from"], raw); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.Pool.Exec(ctx, `INSERT INTO pricing_quote_policies(version,policy) SELECT 'usd-character-unaudited',policy FROM pricing_quote_policies WHERE version=$1`, job.Quote.PricingVersion); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ActivateDue(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var activated bool
	if err := f.DB.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pricing_activation_events WHERE version='usd-character-unaudited')`).Scan(&activated); err != nil || activated {
		t.Fatalf("unaudited price activated=%v %v", activated, err)
	}
	if _, err := f.DB.Pool.Exec(ctx, `INSERT INTO pricing_activation_events(version) VALUES('usd-character-unaudited')`); err != nil {
		t.Fatal(err)
	}
	if s.CreateAvailability(ctx).Enabled {
		t.Fatal("unaudited price enabled creation")
	}
	if _, err := pricing.NewQuotes(f.DB.Pool).Create(ctx, owner, pricing.QuoteInput{Action: "character_create"}); err == nil {
		t.Fatal("unaudited price allowed generic quote")
	}
}
