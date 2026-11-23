package backuptest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LegacyPricingBaseline is the catalogue active before the Hy4 approval.
const LegacyPricingBaseline = "usd-pre-hy4-baseline"

// ActivateLegacyPricingBaseline re-creates the pre-Hy4 catalogue as the newest active version, for tests
// that exercise drafts, approvals and rollbacks on top of that state. Its route
// is the current Hy4 route because the historical two-attempt Sonnet route no
// longer passes the protected-cost gate.
func ActivateLegacyPricingBaseline(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	ActivateLegacyPricingBaselineWithRoute(t, pool, "usd-hy4-check-in-2026-11-22")
}

// ActivateLegacyPricingBaselineWithRoute is ActivateLegacyPricingBaseline with the quote policy copied from
// routeVersion, for tests of historical routes such as usd-quotes-2026-11-09 (Sonnet/Haiku).
func ActivateLegacyPricingBaselineWithRoute(t testing.TB, pool *pgxpool.Pool, routeVersion string) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO pricing_versions(version,effective_from,approved,config)
 SELECT '` + LegacyPricingBaseline + `','2026-11-22T12:00:05Z',true,jsonb_set(jsonb_set(config,'{version}','"` + LegacyPricingBaseline + `"'),'{effective_from}','"2026-11-22T12:00:05Z"')
 FROM pricing_versions WHERE version='usd-quotes-2026-11-09'`,
		`INSERT INTO pricing_quote_policies(version,policy) SELECT '` + LegacyPricingBaseline + `',policy FROM pricing_quote_policies WHERE version='` + routeVersion + `'`,
		`INSERT INTO pricing_activation_events(version,activated_at) VALUES('` + LegacyPricingBaseline + `','2026-11-22T12:00:05Z')`,
	} {
		if _, err := pool.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}
