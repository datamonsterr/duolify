package narrationtest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActivateOpenRouterCostPolicy exercises the real approval flow on a disposable database.
func ActivateOpenRouterCostPolicy(ctx context.Context, pool *pgxpool.Pool) error {
	const version = "usd-openrouter-narration-cost-fixture"
	row, err := db.New(pool).ActiveQuoteConfig(ctx)
	if err != nil {
		return err
	}
	var input map[string]json.RawMessage
	if err = json.Unmarshal(row.Config, &input); err != nil {
		return err
	}
	if string(input["version"]) == fmt.Sprintf("%q", version) {
		return nil
	}
	var actor string
	if err = pool.QueryRow(ctx, `INSERT INTO learners(display_name) VALUES('Narration pricing test admin') RETURNING id::text`).Scan(&actor); err != nil {
		return err
	}
	if _, err = pool.Exec(ctx, `INSERT INTO pricing_admin_grants(account_id) VALUES($1)`, actor); err != nil {
		return err
	}
	admin := pricing.NewAdmin(pool, nil, actor)
	admin.SetEnvironment("development")
	admin.SetVerifier(&pricing.DevelopmentVerifier{})
	effective := time.Now().Add(2 * time.Second).UTC().Truncate(time.Microsecond)
	input["version"], _ = json.Marshal(version)
	input["effective_from"], _ = json.Marshal(effective)
	input["quote_policy_source"] = json.RawMessage(`"usd-openrouter-narration-cost-unapproved"`)
	base := "/admin/v1/pricing/drafts"
	reason := "fixture approval of OpenRouter audio cost policy"
	call := func(path string, payload map[string]any, expected int) error {
		body, _ := json.Marshal(payload)
		status, out := admin.ExecuteCLI(ctx, actor, http.MethodPost, path, body)
		if status != expected {
			return fmt.Errorf("pricing %s: %d %s", path, status, out)
		}
		return nil
	}
	if err = call(base, map[string]any{"config": input, "reason": reason}, http.StatusCreated); err != nil {
		return err
	}
	for _, transition := range []string{"submit", "approve"} {
		if err = call(base+"/"+version+"/"+transition, map[string]any{"reason": reason}, http.StatusOK); err != nil {
			return err
		}
	}
	timer := time.NewTimer(time.Until(effective) + 100*time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	q := db.New(pool)
	catalogue, err := pricing.Load(ctx, q)
	if err != nil {
		return err
	}
	return pricing.NewRuntimeCatalogue(catalogue, q, pool, nil).ActivateDue(ctx, admin)
}
