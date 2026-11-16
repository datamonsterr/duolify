package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This route exists only in the disposable Playwright server. It runs
// the real admin workflow and activator against synthetic accounts and mappings.
func registerCharacterPricingFixture(mux *http.ServeMux, auth *accounts.Service, pool *pgxpool.Pool) {
	mux.Handle("POST /test/characters/approve-price", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		owner := accounts.LearnerID(ctx)
		if err := os.Setenv("PRICING_OWNER_ID", owner); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if _, err := pool.Exec(ctx, `INSERT INTO pricing_admin_grants(account_id) VALUES($1) ON CONFLICT DO NOTHING`, owner); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		raw, err := os.ReadFile("internal/pricing/fixtures/usd_character_create.json")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var cfg map[string]any
		if err := json.Unmarshal(raw, &cfg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		effective := time.Now().UTC().Add(10 * time.Second)
		cfg["effective_from"] = effective.Format(time.RFC3339Nano)
		admin := pricing.NewAdmin(pool, nil, owner)
		admin.SetEnvironment("development")
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
		runtime.SetEnvironment("development")
		runtime.SetOwnerID(owner)
		if err := runtime.ActivateDue(ctx, admin); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))
}
