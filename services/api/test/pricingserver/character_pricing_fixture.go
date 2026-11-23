package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const characterPricingOwner = "00000000-0000-4000-8000-000000000013"

// Each acceptance scenario owns a migrated database, independent of the shared
// catalogue and its mock-only bypass. Pools live until this disposable server
// exits; its Postgres container owns database cleanup, including failed tests.
func registerCharacterPricingScenarios(mux *http.ServeMux, auth *accounts.Service, pool *pgxpool.Pool) func() {
	type scenario struct {
		owner   string
		pool    *pgxpool.Pool
		handler http.Handler
	}
	var scenarios sync.Map
	mux.Handle("POST /test/characters/pricing-fixtures", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		name := "character_pricing_" + id
		if _, err := pool.Exec(r.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		databaseURL, err := url.Parse(pool.Config().ConnString())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		databaseURL.Path = "/" + name
		if err := migrations.Up(r.Context(), databaseURL.String()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		isolated, err := pgxpool.New(r.Context(), databaseURL.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		handler, err := characterPricingScenario(r.Context(), auth, isolated)
		if err != nil {
			isolated.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		path := "/test/characters/pricing-fixtures/" + id
		scenarios.Store(id, scenario{
			owner: accounts.LearnerID(r.Context()), pool: isolated,
			handler: http.StripPrefix(path, handler),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"path": path})
	})))
	mux.Handle("/test/characters/pricing-fixtures/{id}/{path...}", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, ok := scenarios.Load(r.PathValue("id"))
		if !ok || value.(scenario).owner != accounts.LearnerID(r.Context()) {
			http.NotFound(w, r)
			return
		}
		value.(scenario).handler.ServeHTTP(w, r)
	})))
	return func() {
		scenarios.Range(func(_, value any) bool {
			value.(scenario).pool.Close()
			return true
		})
	}
}

func characterPricingScenario(ctx context.Context, auth *accounts.Service, pool *pgxpool.Pool) (http.Handler, error) {
	for _, id := range []string{accounts.LearnerID(ctx), characterPricingOwner} {
		if _, err := pool.Exec(ctx, `INSERT INTO learners(id,display_name) VALUES($1,'character pricing fixture')`, id); err != nil {
			return nil, err
		}
	}
	catalogue, err := pricing.Load(ctx, db.New(pool))
	if err != nil {
		return nil, err
	}
	wallet := credits.NewServiceWithCatalogue(pool, nil, catalogue, nil)
	service := characters.NewService(pool, nil, nil)
	service.ConfigureScreening(characterScreenFixture{})
	service.ConfigureCreation(wallet, characterMapFixture{}, "production", false)
	runtime := pricing.NewRuntimeCatalogue(catalogue, db.New(pool), pool, nil)
	runtime.SetEnvironment("production")
	runtime.SetOwnerID(characterPricingOwner)
	mux := http.NewServeMux()
	registerCharacterPricingFixture(mux, auth, pool)
	mux.Handle("/v1/characters", characters.NewHandler(service))
	mux.Handle("/v1/characters/", characters.NewHandler(service))
	mux.Handle("GET /v1/pricing", runtime)
	mux.Handle("GET /v1/credits/wallet", wallet)
	return mux, nil
}

// This route exists only in the disposable Playwright server. It runs
// the real admin workflow and activator against synthetic accounts and mappings.
func registerCharacterPricingFixture(mux *http.ServeMux, auth *accounts.Service, pool *pgxpool.Pool) {
	mux.Handle("POST /test/characters/approve-price", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := characterPricingOwner
		ctx := accounts.WithLearnerID(r.Context(), owner)
		if _, err := pool.Exec(ctx, `INSERT INTO pricing_admin_grants(account_id) VALUES($1) ON CONFLICT DO NOTHING`, owner); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// Model the existing E16 catalogue before approving the two successors.
		// Only this disposable baseline is seeded; both owner decisions below go
		// through the real draft, submit, approve and activation workflow.
		baselineRaw, err := os.ReadFile("internal/pricing/fixtures/usd_outline_replan.json")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var baseline map[string]any
		if err := json.Unmarshal(baselineRaw, &baseline); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		baseline["version"] = "usd-character-pricing-baseline"
		baseline["effective_from"] = time.Now().UTC().Truncate(time.Microsecond).Add(-time.Second).Format(time.RFC3339Nano)
		baseline["limits"].(map[string]any)["free_concurrent_generation"] = 1
		baselineRaw, _ = json.Marshal(baseline)
		for _, statement := range []string{
			`INSERT INTO pricing_versions(version,effective_from,approved,config) SELECT $1::jsonb->>'version',($1::jsonb->>'effective_from')::timestamptz,true,$1::jsonb`,
			// Current route policy: the historical Sonnet preview route exceeds the enforced preview budget.
			`INSERT INTO pricing_quote_policies(version,policy) SELECT $1::jsonb->>'version',policy FROM pricing_quote_policies WHERE version='usd-voice-preview-2026-11-23'`,
			`INSERT INTO pricing_activation_events(version) SELECT $1::jsonb->>'version'`,
		} {
			if _, err := pool.Exec(ctx, statement, baselineRaw); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		designRaw, err := os.ReadFile("internal/pricing/fixtures/usd_design_chat_owner_approved.json")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		characterRaw, err := os.ReadFile("internal/pricing/fixtures/usd_character_create.json")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		for _, raw := range [][]byte{designRaw, characterRaw} {
			var cfg map[string]any
			if err := json.Unmarshal(raw, &cfg); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			effective := time.Now().UTC().Add(5 * time.Second)
			cfg["effective_from"] = effective.Format(time.RFC3339Nano)
			admin := pricing.NewAdmin(pool, nil, owner)
			admin.SetEnvironment("production")
			admin.SetVerifier(&pricing.DevelopmentVerifier{})
			operations := []struct {
				path    string
				payload any
				want    int
			}{
				{"/admin/v1/pricing/drafts", map[string]any{"config": cfg, "reason": "ADR 0013 acceptance fixture"}, http.StatusCreated},
				{fmt.Sprintf("/admin/v1/pricing/drafts/%s/submit", cfg["version"]), map[string]string{"reason": "submit fixture"}, http.StatusOK},
				{fmt.Sprintf("/admin/v1/pricing/drafts/%s/approve", cfg["version"]), map[string]string{"reason": "owner approves fixture"}, http.StatusOK},
			}
			for _, op := range operations {
				body, _ := json.Marshal(op.payload)
				code, out := admin.ExecuteCLI(ctx, owner, http.MethodPost, op.path, body)
				if code != op.want {
					http.Error(w, fmt.Sprintf("%s: %d %s", op.path, code, out), 500)
					return
				}
			}
			timer := time.NewTimer(time.Until(effective) + 10*time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return
			}
			cat, err := pricing.Load(ctx, db.New(pool))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			runtime := pricing.NewRuntimeCatalogue(cat, db.New(pool), pool, nil)
			runtime.SetEnvironment("production")
			runtime.SetOwnerID(owner)
			if err := runtime.ActivateDue(ctx, admin); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})))
}
