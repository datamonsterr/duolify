//go:build integration

package pricing_test

import (
	"encoding/json"
	"reflect"
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

const (
	approvedHy4 = "usd-hy4-routing-2026-11-22"
	// activeVersion is the owner-approved check-in successor of approvedHy4 (ADR 0020) and is what quotes resolve to.
	activeVersion = "usd-hy4-check-in-2026-11-22"
	// hy4Source is declared in approve_policy_carrying_draft_integration_test.go.
)

// The owner-approved Hy4/GLM version is what quotes resolve to after migrations: every AI action is
// routed to Hy4 with GLM as fallback, learner credit prices are those of the previous approved
// snapshot, usage settles at each model's own rate and a Claude model is refused.
func TestApprovedHy4RoutingVersionIsActiveAndQuotesTheEngineRoute(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("approved_hy4"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	// Running twice proves the migration is idempotent.
	for range 2 {
		if err = migrations.Up(ctx, url); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	active, err := db.Queries.ActiveQuoteConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pricing.Parse(active.Config)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != activeVersion {
		t.Fatalf("active quote version %q, want %q", cfg.Version, activeVersion)
	}
	var policy pricing.QuotePolicy
	if err = json.Unmarshal(active.Policy, &policy); err != nil {
		t.Fatal(err)
	}
	for action := range cfg.Actions {
		if action == "narration" || action == "cached_replay" {
			continue
		}
		primary, fallback := policy.Models[policy.ActionModels[action]], policy.Models[policy.FallbackModels[policy.ActionModels[action]]]
		if primary.Provider != "openrouter" || primary.Model != "tencent/hy4-flash" || fallback.Provider != "openrouter" || fallback.Model != "z-ai/glm-5.3-flash" {
			t.Errorf("%s routes %s/%s -> %s/%s", action, primary.Provider, primary.Model, fallback.Provider, fallback.Model)
		}
	}

	// The approved row carries the unapproved row's quote policy and every learner-facing term; only the
	// version name differs. The unapproved row is untouched and never active.
	var sameConfig, samePolicy, unapproved, unapprovedActivated bool
	if err = db.Pool.QueryRow(ctx, `SELECT
 (SELECT config-'version'-'effective_from' FROM pricing_versions WHERE version=$1)=(SELECT config-'version'-'effective_from' FROM pricing_versions WHERE version=$2),
 (SELECT policy FROM pricing_quote_policies WHERE version=$1)=(SELECT policy FROM pricing_quote_policies WHERE version=$2),
 NOT (SELECT approved FROM pricing_versions WHERE version=$2),
 EXISTS(SELECT 1 FROM pricing_activation_events WHERE version=$2)`, approvedHy4, hy4Source).Scan(&sameConfig, &samePolicy, &unapproved, &unapprovedActivated); err != nil {
		t.Fatal(err)
	}
	if !sameConfig || !samePolicy || !unapproved || unapprovedActivated {
		t.Fatalf("config %v policy %v unapproved-untouched %v unapproved-activated %v", sameConfig, samePolicy, unapproved, unapprovedActivated)
	}

	// Learner prices equal the previous approved snapshot (the newest approved version before this one). A
	// fresh database's previous snapshot predates the design-chat and character actions, so only its actions are compared.
	var previousRaw []byte
	if err = db.Pool.QueryRow(ctx, `SELECT config FROM pricing_versions WHERE approved AND version<>$1 ORDER BY effective_from DESC LIMIT 1`, activeVersion).Scan(&previousRaw); err != nil {
		t.Fatal(err)
	}
	previous, err := pricing.Parse(previousRaw)
	if err != nil {
		t.Fatal(err)
	}
	for key, action := range previous.Actions {
		if cfg.Actions[key].Credits != action.Credits {
			t.Errorf("%s credits %d, previous snapshot %s has %d", key, cfg.Actions[key].Credits, previous.Version, action.Credits)
		}
	}
	if !reflect.DeepEqual(cfg.Packs, previous.Packs) || !reflect.DeepEqual(cfg.Plans, previous.Plans) || cfg.DailyFree.Credits != previous.DailyFree.Credits {
		t.Errorf("packs, plans or daily free credits differ from %s", previous.Version)
	}

	// Activation rows are the ones ActivateDue writes, and ActivateDue has nothing left to do.
	var effective, activatedAt time.Time
	var events, activations, evidence int
	if err = db.Pool.QueryRow(ctx, `SELECT v.effective_from, coalesce(e.activated_at,'epoch'),
 (SELECT count(*) FROM pricing_activation_events WHERE version=$1),
 (SELECT count(*) FROM pricing_audit WHERE version=$1 AND action='version_activated'),
 (SELECT count(*) FROM pricing_verification_evidence WHERE version=$1)
 FROM pricing_versions v LEFT JOIN pricing_activation_events e ON e.version=v.version WHERE v.version=$1 AND v.approved`, approvedHy4).Scan(&effective, &activatedAt, &events, &activations, &evidence); err != nil {
		t.Fatal(err)
	}
	if events != 1 || activations != 1 || evidence != 1 || !activatedAt.Equal(effective) || effective.After(time.Now()) {
		t.Fatalf("events %d activations %d evidence %d activated %v effective %v", events, activations, evidence, activatedAt, effective)
	}
	if err = pricing.ValidateEconomics(active.Config, &pricing.DevelopmentVerifier{}, ""); err != nil {
		t.Fatalf("economics and mappings: %v", err)
	}
	var owner string
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('pricing owner') RETURNING id::text").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	admin := pricing.NewAdmin(db.Pool, nil, owner)
	admin.SetVerifier(&pricing.DevelopmentVerifier{})
	runtime := pricing.NewRuntimeCatalogue(mustLoad(t, db), db.Queries, db.Pool, nil)
	if err = runtime.ActivateDue(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var after int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_activation_events WHERE version=$1", approvedHy4).Scan(&after); err != nil || after != 1 {
		t.Fatalf("activation events after ActivateDue: %d %v", after, err)
	}

	// A preview quote resolves to the approved version and settles each model at its own rate.
	var learner string
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('quote learner') RETURNING id::text").Scan(&learner); err != nil {
		t.Fatal(err)
	}
	q, err := pricing.NewQuotes(db.Pool).Create(ctx, learner, pricing.QuoteInput{Action: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if q.PricingVersion != activeVersion || q.Rate.Provider != "openrouter" || q.Rate.Model != "tencent/hy4-flash" {
		t.Fatalf("preview quote %s %+v", q.PricingVersion, q.Rate)
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
	for _, model := range []string{"anthropic/claude-sonnet-5.5", "claude-sonnet-5-5", "anthropic/claude-haiku-5.5"} {
		if _, err := credits.CalculateSettlement(q, usage(model, true)); err == nil {
			t.Errorf("claude model %s must be rejected", model)
		}
	}
}

// legacyBaseline is the Sonnet/Haiku catalogue that was active before the Hy4 approval, re-created as the
// newest active version for tests that exercise activation, drafts and rollbacks on top of that state.
const legacyBaseline = "usd-pre-hy4-baseline"

func activateLegacyBaseline(t *testing.T, db *store.Store) {
	t.Helper()
	ctx := t.Context()
	for _, statement := range []string{
		`INSERT INTO pricing_versions(version,effective_from,approved,config)
 SELECT '` + legacyBaseline + `','2026-11-22T12:00:03Z',true,jsonb_set(jsonb_set(config,'{version}','"` + legacyBaseline + `"'),'{effective_from}','"2026-11-22T12:00:03Z"')
 FROM pricing_versions WHERE version='usd-quotes-2026-11-09'`,
		`INSERT INTO pricing_quote_policies(version,policy) SELECT '` + legacyBaseline + `',policy FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09'`,
		`INSERT INTO pricing_activation_events(version,activated_at) VALUES('` + legacyBaseline + `','2026-11-22T12:00:03Z')`,
	} {
		if _, err := db.Pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
}
