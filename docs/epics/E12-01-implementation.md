# E12-01 implementation report

Issue: #29. Branch: `so/e12-01-d86bf84b`.

## Delivered

- Timestamped Goose migration seeds the approved USD launch catalogue, all
  action/pack/plan/benefit/limit values and explicit synthetic development SKUs.
- JSON Schema and cross-field validation run before API startup. The endpoint
  serves version/effective time, next scheduled data, ETag/304 and max-age=60.
  Public output excludes mappings and always disables sales.
- Wallet policy/empty activity, pack/plan comparison, billing periods and course
  creation cost breakdown use server data and semantic design variables.
  Session-cached offline prices have an explicit last-known/version label.
- Pricing exposure uses the existing PostHog wrapper with UUID identity, common
  properties, opt-out and session/surface/version dedupe. Catalogue additions
  accompany emitters. Source checks block price literals in the client build.
- Immutable pricing data is inventoried. Postgres 18 tests verify exact seed
  equality, 140 + 50 = 190, daily 46, mutation rejection, account deletion and
  real custom-format dump/restore resolver equality. No AI behavior changes.

## Files changed

- `.mise/tasks/api.toml`, `.mise/tasks/client.toml`
- `.github/workflows/_checks.yml` (Go setup for real API Playwright fixture)
- `apps/client/.prettierignore`, `apps/client/playwright.config.ts`
- `apps/client/e2e/pricing-e12-01.spec.ts`
- `apps/client/src/App.tsx`, `apps/client/src/lib/api.ts`,
  `apps/client/src/lib/analytics.ts`
- `apps/client/src/features/pricing/PricingView.tsx`, `pricing.css`, `types.ts`,
  `fixtures/approved.json`, `catalogue-boundary.test.ts`,
  `serve_the_approved_pricing_seed_end_to_end.test.tsx`
- `services/api/cmd/api/main.go`, `services/api/go.mod`, `services/api/go.sum`
- `services/api/internal/httpapi/router.go`, `middleware.go`
- `services/api/internal/pricing/pricing.go`, `schema.json`, `README.md`,
  `serve_the_approved_pricing_seed_end_to_end_test.go`,
  `serve_the_approved_pricing_seed_end_to_end_integration_test.go`
- `services/api/migrations/20261109054917_pricing_seed.sql`
- `services/api/queries/pricing.sql`, `services/api/internal/store/db/pricing.sql.go`,
  `services/api/internal/store/db/models.go` (sqlc-generated)
- `services/api/test/pricingserver/main.go`
- `openspec/specs/analytics/spec.md`, `docs/data-inventory.md`, this report

## Commands and results

Required red sequence was observed: `mise run client:build && mise run client:e2e`
failed on missing price views; `mise exec -- go test -tags integration
./internal/pricing/...` (from services/api) failed on the missing pricing package;
`mise run api:test` and `mise run client:test` failed on missing implementation.
Implementation and fixture errors found during subsequent runs were corrected.

Final verification:

| Exact command | Result |
| --- | --- |
| `mise run api:generate` | PASS; pinned sqlc regenerated |
| `mise run api:lint` | PASS; zero issues |
| `mise run api:test` | PASS; 35 tests, race detector |
| `mise run api:build` | PASS |
| `mise run api:test-e2e` | PASS; 10 tests |
| `mise run api:test-integration` | PASS; 18 tests, Postgres 18 Docker |
| `mise run client:lint-web` | PASS; ESLint, Prettier, TypeScript |
| `mise run client:test-web` | PASS; 9 tests |
| `mise run client:build` | PASS; catalogue boundary test plus production build |
| `mise run client:e2e` | PASS; 6 tests, real migrated Postgres/API; desktop, 390/320px, touch, keyboard, loading, empty wallet, error/retry, offline, long content, light/dark and reduced motion |
| `mise run client:lint` | FAIL at native Clippy only: missing `javascriptcoregtk-4.1`; JS checks and Rust formatting passed |
| `mise run client:test` | FAIL at native Cargo test only: missing `javascriptcoregtk-4.1`; web tests passed |
| `mise run ci:tauri-deps` | FAIL: host sudo requires a password |
| `mise run ci:openspec` | PASS; all 21 items, strict |
| `mise run ci:lint` | PASS |
| `git diff --check` | PASS |

Other successful setup/format commands: `mise trust`, `mise tasks`,
`mise exec -- go get github.com/santhosh-tekuri/jsonschema/v6@v6.0.2`,
`mise exec -- go mod tidy` (both from services/api), `mise run api:format`,
`mise run client:format-pricing`. No secrets or new environment variables.

## Deferred boundaries

Parent explicitly approved native checks running in CI because this host lacks
WebKit/JavaScriptCore development libraries and no `src-tauri` files changed.
No tests were weakened or skipped in the standard tasks.

Parent explicitly accepted the auth integration boundary: E1-02 will supply the
authenticated session to `configureAnalytics`. Signed-out catalogue browsing
does not invent Learner UUIDs.

Live sales remain disabled pending E12-07/E12-08 verified provider mappings.
Subsequent E12 tickets own admin scheduling/runtime activation, quoting,
settlement and real wallet balances. Existing AI eval/fallback gates are intact.
