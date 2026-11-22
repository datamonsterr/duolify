//go:build integration

package pricing_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const hy4Source = "usd-hy4-routing-2026-11-22-unapproved"

// The owner approves a draft that points at the unapproved Hy4/GLM version's quote policy through
// the normal admin path. The draft is non-adverse (same prices), the unapproved row never becomes
// active, and quotes then settle Hy4 and GLM usage at their own rates.
func TestOwnerApprovesAPolicyCarryingDraftThroughTheAdminPath(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("policy_carrying"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	var owner, learner string
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('pricing owner') RETURNING id::text").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('quote learner') RETURNING id::text").Scan(&learner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", owner); err != nil {
		t.Fatal(err)
	}
	admin := pricing.NewAdmin(db.Pool, nil, owner)
	admin.SetVerifier(&pricing.DevelopmentVerifier{})
	admin.SetEnvironment("production")
	defer admin.SetEnvironment("development")

	var raw []byte
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version=$1", hy4Source).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	config := func(version string, effective time.Time) map[string]any {
		var c map[string]any
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		// The active parent carries the owner-approved check-in block (ADR 0020); dropping it would be adverse.
		c["check_in"] = map[string]any{"daily_credits": 150, "first_time_bonus": 250, "bank_days": 7}
		c["version"], c["effective_from"] = version, effective.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
		return c
	}
	call := func(path string, payload any) (int, string) {
		t.Helper()
		body, _ := json.Marshal(payload)
		status, out := admin.ExecuteCLI(ctx, owner, http.MethodPost, path, body)
		return status, string(out)
	}
	draft := func(version, source string) (int, string) {
		c := config(version, time.Now().Add(3*time.Second))
		if source != "" {
			c["quote_policy_source"] = source
		}
		encoded, _ := json.Marshal(c)
		return call("/admin/v1/pricing/drafts", map[string]any{"config": json.RawMessage(encoded), "reason": "route AI through Hy4 and GLM"})
	}

	// A source with dearer terms must be adverse even when the catalogue prices are unchanged.
	const expensive = "usd-policy-carry-expensive"
	if _, err = db.Pool.Exec(ctx, `INSERT INTO pricing_versions(version,effective_from,approved,config)
 SELECT $1,now()+interval '200 hours',false,jsonb_set(jsonb_set(config,'{version}',to_jsonb($1::text)),'{effective_from}',to_jsonb(now()+interval '200 hours')) FROM pricing_versions WHERE version=$2`, expensive, hy4Source); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO pricing_quote_policies(version,policy)
 SELECT $1,jsonb_set(policy,'{usd_nanos_per_credit}','1'::jsonb) FROM pricing_quote_policies WHERE version=$2`, expensive, hy4Source); err != nil {
		t.Fatal(err)
	}
	if status, out := draft("usd-policy-carry-adverse", expensive); status != http.StatusCreated {
		t.Fatalf("adverse draft: %d %s", status, out)
	}
	var adverseClass string
	if err = db.Pool.QueryRow(ctx, `SELECT change_class FROM pricing_drafts WHERE version='usd-policy-carry-adverse'`).Scan(&adverseClass); err != nil || adverseClass != "adverse" {
		t.Fatalf("adverse policy classified %q: %v", adverseClass, err)
	}
	if status, out := call("/admin/v1/pricing/drafts/usd-policy-carry-adverse/submit", map[string]string{"reason": "test"}); status != http.StatusOK {
		t.Fatalf("submit: %d %s", status, out)
	}
	if status, out := call("/admin/v1/pricing/drafts/usd-policy-carry-adverse/approve", map[string]string{"reason": "test"}); status == http.StatusOK {
		t.Fatalf("adverse policy bypassed notice: %s", out)
	}

	// A draft naming an unknown policy source is refused before approval.
	if status, out := draft("usd-policy-carry-bad", "usd-does-not-exist"); status == http.StatusCreated {
		t.Fatalf("unknown policy source accepted: %s", out)
	}
	// Without a source the draft inherits its parent's policy (the already approved Hy4 version).
	if status, out := draft("usd-policy-carry-inherit", ""); status != http.StatusCreated {
		t.Fatalf("inherit draft: %d %s", status, out)
	}

	if status, out := draft("usd-policy-carry-hy4", hy4Source); status != http.StatusCreated {
		t.Fatalf("draft: %d %s", status, out)
	}
	var class string
	if err = db.Pool.QueryRow(ctx, "SELECT change_class FROM pricing_drafts WHERE version='usd-policy-carry-hy4'").Scan(&class); err != nil || class != "non_adverse" {
		t.Fatalf("a policy source with unchanged prices must be non-adverse: %q %v", class, err)
	}
	for _, op := range []string{"submit", "approve"} {
		if status, out := call("/admin/v1/pricing/drafts/usd-policy-carry-hy4/"+op, map[string]string{"reason": "owner approves Hy4 routing"}); status != http.StatusOK {
			t.Fatalf("%s: %d %s", op, status, out)
		}
	}
	var same bool
	if err = db.Pool.QueryRow(ctx, `SELECT (SELECT policy FROM pricing_quote_policies WHERE version='usd-policy-carry-hy4')=(SELECT policy FROM pricing_quote_policies WHERE version=$1)`, hy4Source).Scan(&same); err != nil || !same {
		t.Fatalf("approved version must pin the source policy: %v %v", same, err)
	}

	// Not active before its effective instant; the approved Hy4 version serves quotes meanwhile and the
	// unapproved source never does.
	runtime := pricing.NewRuntimeCatalogue(mustLoad(t, db), db.Queries, db.Pool, nil)
	quotes := pricing.NewQuotes(db.Pool)
	before, err := quotes.Create(ctx, learner, pricing.QuoteInput{Action: "quick_note"})
	if err != nil || before.PricingVersion != activeVersion || before.PricingVersion == hy4Source {
		t.Fatalf("quote before activation: %+v %v", before.Rate, err)
	}
	time.Sleep(3500 * time.Millisecond)
	if err = runtime.ActivateDue(ctx, admin); err != nil {
		t.Fatal(err)
	}
	q, err := quotes.Create(ctx, learner, pricing.QuoteInput{Action: "quick_note"})
	if err != nil {
		t.Fatal(err)
	}
	if q.PricingVersion != "usd-policy-carry-hy4" || q.Rate.Model != "tencent/hy4-flash" {
		t.Fatalf("quote after activation: %s %+v", q.PricingVersion, q.Rate)
	}
	var sourceApproved bool
	if err = db.Pool.QueryRow(ctx, "SELECT approved FROM pricing_versions WHERE version=$1", hy4Source).Scan(&sourceApproved); err != nil || sourceApproved {
		t.Fatalf("the source version must stay unapproved: %v %v", sourceApproved, err)
	}

	usage := func(model string, fallback bool) []*enginev1.Usage {
		return []*enginev1.Usage{{Provider: "openrouter", Model: model, Attempt: 1, Component: "content", Delivered: true, FallbackUsed: fallback, InputTokens: 1000, OutputTokens: 300}}
	}
	if got, err := credits.CalculateSettlement(q, usage("tencent/hy4-flash", false)); err != nil || got.CostNanos != 1000*35+300*141 {
		t.Fatalf("hy4 settlement: %+v %v", got, err)
	}
	if got, err := credits.CalculateSettlement(q, usage("z-ai/glm-5.3-flash", true)); err != nil || got.CostNanos != 1000*40+300*79 {
		t.Fatalf("glm settlement: %+v %v", got, err)
	}
	if _, err := credits.CalculateSettlement(q, usage("anthropic/claude-haiku-5.5", true)); err == nil {
		t.Fatal("a model outside the route must be rejected")
	}
}

func mustLoad(t *testing.T, db *store.Store) *pricing.Catalogue {
	t.Helper()
	c, err := pricing.Load(t.Context(), db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
